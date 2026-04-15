package main

import (
	"bytes"
	"compress/flate"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"flag"
	"log"
	"math/rand/v2"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	intcrypto "xfeed/server/internal/crypto"
	"xfeed/server/internal/feed"
)

const maxTXTChunkLen = 220

func main() {
	var (
		listen           = flag.String("listen", ":5300", "UDP listen address")
		domain           = flag.String("domain", "t.example.com", "Tunnel domain suffix")
		mode             = flag.String("mode", "no-telegram", "Server mode")
		compression      = flag.String("compression", "deflate", "Compression mode")
		channelsFile     = flag.String("channels-file", "/var/lib/thefeed/channels.txt", "Telegram channels list")
		xrssFile         = flag.String("x-rss-file", "/var/lib/thefeed/x_sources.txt", "X RSS source list")
		paddingMin       = flag.Int("padding-min", 12, "Minimum random padding bytes")
		paddingMax       = flag.Int("padding-max", 56, "Maximum random padding bytes")
		maxResponseBytes = flag.Int("max-dns-response-bytes", 512, "Max DNS response bytes")
		stressParts      = flag.Int("stress-multipart-parts", 0, "Force fixed multipart count for lab testing")
		stressFillBytes  = flag.Int("stress-fill-bytes", 0, "Append filler bytes before compression for stress testing")
		disableLogs      = flag.Bool("disable-persistent-logs", true, "Disable persistent logs")
	)
	flag.Parse()
	_ = mode
	_ = compression
	_ = disableLogs
	masterKey, err := intcrypto.ParseKey32(os.Getenv("THEFEED_MASTER_KEY"))
	if err != nil {
		log.Fatalf("invalid THEFEED_MASTER_KEY: %v", err)
	}
	feedService, err := feed.NewService(*channelsFile, *xrssFile)
	if err != nil {
		log.Fatalf("feed service init: %v", err)
	}

	// Pre-compute encrypted chunks and refresh periodically.
	chunkCache := &chunkCache{}
	refreshChunks := func() {
		items, err := feedService.Latest(context.Background(), 6)
		if err != nil {
			items = []feed.Item{{
				Source: "system",
				Text:   "feed temporarily unavailable",
				Time:   time.Now().UTC().Format(time.RFC3339),
			}}
		}
		payloadJSON, _ := json.Marshal(items)
		if *stressFillBytes > 0 {
			payloadJSON = append(payloadJSON, []byte(strings.Repeat("Z", *stressFillBytes))...)
		}
		deflated := deflatePayload(payloadJSON)
		encrypted, err := encryptAESGCM(masterKey, deflated)
		if err != nil {
			return
		}
		encoded := base64.RawURLEncoding.EncodeToString(encrypted)
		chunks := splitFixed(encoded, maxTXTChunkLen)
		if *stressParts > 1 {
			chunks = splitForTargetParts(encoded, *stressParts)
		}
		chunkCache.set(chunks)
	}
	refreshChunks()
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			refreshChunks()
		}
	}()

	conn, err := net.ListenPacket("udp", *listen)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	defer conn.Close()
	log.Printf("thefeed listening on %s", *listen)

	buf := make([]byte, 2048)
	for {
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			continue
		}
		resp, ok := buildTXTResponse(
			buf[:n],
			*domain,
			chunkCache,
			*paddingMin,
			*paddingMax,
			*maxResponseBytes,
		)
		if !ok {
			continue
		}
		_, _ = conn.WriteTo(resp, addr)
	}
}

type chunkCache struct {
	mu     sync.RWMutex
	chunks []string
}

func (c *chunkCache) set(chunks []string) {
	c.mu.Lock()
	c.chunks = chunks
	c.mu.Unlock()
}

func (c *chunkCache) get() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.chunks
}

func buildTXTResponse(
	query []byte,
	domain string,
	cache *chunkCache,
	padMin,
	padMax,
	maxBytes int,
) ([]byte, bool) {
	if len(query) < 12 {
		return nil, false
	}
	id := binary.BigEndian.Uint16(query[0:2])
	flags := uint16(0x8180) // standard response, recursion available, no error
	qdcount := binary.BigEndian.Uint16(query[4:6])
	if qdcount != 1 {
		return nil, false
	}

	qname, qnameEnd := parseQName(query, 12)
	if qnameEnd <= 0 || qnameEnd+4 > len(query) {
		return nil, false
	}
	qtype := binary.BigEndian.Uint16(query[qnameEnd : qnameEnd+2])
	qclass := binary.BigEndian.Uint16(query[qnameEnd+2 : qnameEnd+4])
	if qtype != 16 || qclass != 1 {
		return nil, false
	}

	normalized := strings.TrimSuffix(strings.ToLower(qname), ".")
	domain = strings.TrimSuffix(strings.ToLower(domain), ".")
	if !strings.HasSuffix(normalized, domain) {
		return nil, false
	}
	partIndex := parsePartIndex(normalized, domain)

	chunks := cache.get()
	if len(chunks) == 0 {
		return nil, false
	}
	if partIndex < 0 || partIndex >= len(chunks) {
		return nil, false
	}
	partHeader := "c:" + intToString(partIndex+1) + "/" + intToString(len(chunks)) + ":"

	txt, ok := buildTXTWithinCap(partHeader, chunks[partIndex], padMin, padMax, maxBytes, len(query))
	if !ok {
		return nil, false
	}

	question := query[12 : qnameEnd+4]
	answer := buildAnswerRdata(txt)

	resp := make([]byte, 12)
	binary.BigEndian.PutUint16(resp[0:2], id)
	binary.BigEndian.PutUint16(resp[2:4], flags)
	binary.BigEndian.PutUint16(resp[4:6], 1)
	binary.BigEndian.PutUint16(resp[6:8], 1)
	// NSCOUNT + ARCOUNT left as zero
	resp = append(resp, question...)
	// Name pointer to question at offset 12.
	resp = append(resp, 0xC0, 0x0C)
	resp = appendUint16(resp, 16) // TXT
	resp = appendUint16(resp, 1)  // IN
	resp = appendUint32(resp, 15) // TTL
	resp = appendUint16(resp, uint16(len(answer)))
	resp = append(resp, answer...)

	if len(resp) > maxBytes {
		return nil, false
	}
	return resp, true
}

func buildTXTWithinCap(partHeader, chunk string, padMin, padMax, maxBytes, queryLen int) (string, bool) {
	paddingLen := padMin
	if padMax > padMin {
		paddingLen = padMin + rand.IntN(padMax-padMin+1)
	}
	for paddingLen >= 0 {
		txt := partHeader + chunk + "." + strings.Repeat("x", paddingLen)
		if len(txt) > 255 {
			txt = txt[:255]
		}
		answer := buildAnswerRdata(txt)
		baseResp := 12 + (queryLen - 12) + 2 + 2 + 2 + 4 + 2 // header + question + rr meta
		if baseResp+len(answer) <= maxBytes {
			return txt, true
		}
		paddingLen--
	}
	return "", false
}

func parsePartIndex(qname, domain string) int {
	trimmed := strings.TrimSuffix(qname, "."+domain)
	labels := strings.Split(trimmed, ".")
	for _, label := range labels {
		if strings.HasPrefix(label, "p") && len(label) > 1 {
			val := label[1:]
			n := 0
			for i := 0; i < len(val); i++ {
				if val[i] < '0' || val[i] > '9' {
					n = -1
					break
				}
				n = n*10 + int(val[i]-'0')
			}
			if n >= 1 {
				return n - 1
			}
		}
	}
	return 0
}

func splitFixed(s string, size int) []string {
	if size <= 0 || len(s) <= size {
		return []string{s}
	}
	out := make([]string, 0, (len(s)+size-1)/size)
	for i := 0; i < len(s); i += size {
		end := i + size
		if end > len(s) {
			end = len(s)
		}
		out = append(out, s[i:end])
	}
	return out
}

func splitForTargetParts(s string, parts int) []string {
	if parts <= 1 || len(s) == 0 {
		return []string{s}
	}
	chunkSize := (len(s) + parts - 1) / parts
	return splitFixed(s, chunkSize)
}

func intToString(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + (v % 10))
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func encryptAESGCM(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	for i := range nonce {
		nonce[i] = byte(rand.IntN(256))
	}
	out := gcm.Seal(nil, nonce, plaintext, nil)
	return append(nonce, out...), nil
}

func buildAnswerRdata(txt string) []byte {
	if len(txt) > 255 {
		txt = txt[:255]
	}
	out := make([]byte, 0, len(txt)+1)
	out = append(out, byte(len(txt)))
	out = append(out, []byte(txt)...)
	return out
}

func parseQName(packet []byte, offset int) (string, int) {
	labels := make([]string, 0, 8)
	for {
		if offset >= len(packet) {
			return "", -1
		}
		l := int(packet[offset])
		offset++
		if l == 0 {
			return strings.Join(labels, "."), offset
		}
		if l > 63 || offset+l > len(packet) {
			return "", -1
		}
		labels = append(labels, string(packet[offset:offset+l]))
		offset += l
	}
}

func deflatePayload(in []byte) []byte {
	var b bytes.Buffer
	w, err := flate.NewWriter(&b, flate.BestSpeed)
	if err != nil {
		return in
	}
	if _, err := w.Write(in); err != nil {
		_ = w.Close()
		return in
	}
	_ = w.Close()
	return b.Bytes()
}

func appendUint16(dst []byte, v uint16) []byte {
	tmp := []byte{0, 0}
	binary.BigEndian.PutUint16(tmp, v)
	return append(dst, tmp...)
}

func appendUint32(dst []byte, v uint32) []byte {
	tmp := []byte{0, 0, 0, 0}
	binary.BigEndian.PutUint32(tmp, v)
	return append(dst, tmp...)
}
