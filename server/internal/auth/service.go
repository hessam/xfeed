package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/pbkdf2"
	"xfeed/server/internal/tokenstore"
)

type tokenRequest struct {
	InviteToken string `json:"invite_token"`
}

type tokenResponse struct {
	Envelope      string `json:"envelope"`
	IV            string `json:"iv"`
	Salt          string `json:"salt"`
	Alg           string `json:"alg"`
	KDF           string `json:"kdf"`
	KDFIterations int    `json:"kdf_iterations"`
	ExpiresIn     int    `json:"expires_in_seconds"`
	KeyHint       string `json:"key_hint"`
}

type adminCreateTokenRequest struct {
	TTLDays int    `json:"ttl_days"`
	Meta    string `json:"meta"`
}

type adminCreateTokenResponse struct {
	InviteToken string `json:"invite_token"`
	ExpiresAt   string `json:"expires_at"`
}

type limiterEntry struct {
	count     int
	windowEnd time.Time
}

type RateLimiter struct {
	mu      sync.Mutex
	rps     int
	entries map[string]*limiterEntry
}

func NewRateLimiter(rps int) *RateLimiter {
	if rps < 1 {
		rps = 1
	}
	return &RateLimiter{rps: rps, entries: map[string]*limiterEntry{}}
}

func (r *RateLimiter) Allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	e, ok := r.entries[key]
	if !ok || now.After(e.windowEnd) {
		r.entries[key] = &limiterEntry{count: 1, windowEnd: now.Add(time.Second)}
		return true
	}
	if e.count >= r.rps {
		return false
	}
	e.count++
	return true
}

type Service struct {
	store          *tokenstore.Store
	tokenHMACKey   []byte
	thefeedKey     string
	adminSecret    string
	consumeOnIssue bool
	limiter        *RateLimiter
}

func NewService(store *tokenstore.Store, tokenHMACKey []byte, thefeedKey, adminSecret string, consumeOnIssue bool, rps int) *Service {
	return &Service{
		store:          store,
		tokenHMACKey:   tokenHMACKey,
		thefeedKey:     thefeedKey,
		adminSecret:    adminSecret,
		consumeOnIssue: consumeOnIssue,
		limiter:        NewRateLimiter(rps),
	}
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/v1/token/exchange", s.exchangeToken)
	mux.HandleFunc("/admin/tokens/create", s.createToken)
	return mux
}

func (s *Service) exchangeToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.TLS == nil {
		http.Error(w, "tls required", http.StatusUpgradeRequired)
		return
	}

	clientIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !s.limiter.Allow(clientIP) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}

	var req tokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	token := strings.TrimSpace(req.InviteToken)
	if token == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	hash := hmac.New(sha256.New, s.tokenHMACKey)
	hash.Write([]byte(token))
	tokenHash := hash.Sum(nil)

	// Constant-time-ish failure behavior: use the same 401 response for all failures.
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.ValidateAndConsume(ctx, tokenHash, s.consumeOnIssue); err != nil {
		time.Sleep(150 * time.Millisecond)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	envelope, iv, salt, err := issueSessionEnvelope(token, s.thefeedKey)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	resp := tokenResponse{
		Envelope:      envelope,
		IV:            iv,
		Salt:          salt,
		Alg:           "AES-256-GCM",
		KDF:           "PBKDF2-SHA256",
		KDFIterations: 120000,
		ExpiresIn:     3600,
		KeyHint:       "derive key from invite_token + salt via PBKDF2-SHA256",
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Service) createToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.adminSecret == "" || r.Header.Get("X-Admin-Secret") != s.adminSecret {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var req adminCreateTokenRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	ttlDays := req.TTLDays
	if ttlDays <= 0 {
		ttlDays = 30
	}
	if req.Meta == "" {
		req.Meta = "admin-issued"
	}

	tokenRaw := make([]byte, 24)
	_, _ = rand.Read(tokenRaw)
	token := base64.RawURLEncoding.EncodeToString(tokenRaw)
	tokenHash := hmac.New(sha256.New, s.tokenHMACKey)
	tokenHash.Write([]byte(token))

	now := time.Now().UTC()
	expires := now.Add(time.Duration(ttlDays) * 24 * time.Hour)
	if err := s.store.Insert(r.Context(), tokenHash.Sum(nil), now.Unix(), expires.Unix(), req.Meta); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(adminCreateTokenResponse{
		InviteToken: token,
		ExpiresAt:   expires.Format(time.RFC3339),
	})
}

func issueSessionEnvelope(inviteToken, masterKey string) (string, string, string, error) {
	salt := make([]byte, 16)
	iv := make([]byte, 12)
	_, _ = rand.Read(salt)
	_, _ = rand.Read(iv)

	derivedKey := pbkdf2.Key([]byte(inviteToken), salt, 120000, 32, sha256.New)
	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return "", "", "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", "", err
	}
	payload := masterKey + "|" + time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	ciphertext := gcm.Seal(nil, iv, []byte(payload), nil)

	return base64.RawURLEncoding.EncodeToString(ciphertext),
		base64.RawURLEncoding.EncodeToString(iv),
		base64.RawURLEncoding.EncodeToString(salt),
		nil
}
