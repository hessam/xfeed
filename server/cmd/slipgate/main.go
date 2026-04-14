package main

import (
	"flag"
	"log"
	"net"
	"strings"
	"time"
)

func main() {
	var (
		listen   = flag.String("listen", ":53", "UDP listen address")
		domain   = flag.String("domain", "t.example.com", "DNS suffix to route")
		upstream = flag.String("upstream", "thefeed:5300", "Upstream UDP target")
	)
	flag.Parse()

	conn, err := net.ListenPacket("udp", *listen)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	defer conn.Close()
	log.Printf("slipgate listening on %s -> %s", *listen, *upstream)

	buf := make([]byte, 2048)
	for {
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			continue
		}
		query := append([]byte(nil), buf[:n]...)
		qname, _ := parseQName(query, 12)
		normalizedQ := strings.TrimSuffix(strings.ToLower(qname), ".")
		normalizedD := strings.TrimSuffix(strings.ToLower(*domain), ".")
		if !strings.HasSuffix(normalizedQ, normalizedD) {
			continue
		}
		resp, ok := forwardUDP(query, *upstream)
		if !ok {
			continue
		}
		_, _ = conn.WriteTo(resp, addr)
	}
}

func forwardUDP(packet []byte, target string) ([]byte, bool) {
	c, err := net.DialTimeout("udp", target, 2*time.Second)
	if err != nil {
		return nil, false
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.Write(packet); err != nil {
		return nil, false
	}
	buf := make([]byte, 2048)
	n, err := c.Read(buf)
	if err != nil {
		return nil, false
	}
	return buf[:n], true
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
