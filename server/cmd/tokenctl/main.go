package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"xfeed/server/internal/tokenstore"
)

func main() {
	var (
		dbPath  = flag.String("db", "/var/lib/thefeed/tokens.db", "SQLite DB path")
		count   = flag.Int("count", 5, "Number of tokens to generate")
		ttlDays = flag.Int("ttl-days", 30, "Token TTL days")
		meta    = flag.String("meta", "bootstrap", "Token metadata tag")
	)
	flag.Parse()

	secret := os.Getenv("TOKEN_HMAC_SECRET")
	if secret == "" {
		log.Fatal("TOKEN_HMAC_SECRET is required")
	}
	store, err := tokenstore.Open(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC().Unix()
	exp := time.Now().UTC().Add(time.Duration(*ttlDays) * 24 * time.Hour).Unix()
	for i := 0; i < *count; i++ {
		token := randomToken(24)
		hash := hmac.New(sha256.New, []byte(secret))
		hash.Write([]byte(token))
		tokenHash := hash.Sum(nil)

		err := store.Insert(context.Background(), tokenHash, now, exp, *meta)
		if err != nil {
			log.Fatalf("insert token: %v", err)
		}
		fmt.Println(token)
	}
}

func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
