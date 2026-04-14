package crypto

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
)

func ParseKey32(in string) ([]byte, error) {
	s := strings.TrimSpace(in)
	if s == "" {
		return nil, errors.New("empty key")
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	// Deterministic fallback for local/dev tokens.
	h := sha256.Sum256([]byte(s))
	return h[:], nil
}
