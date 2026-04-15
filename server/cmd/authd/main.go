package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"xfeed/server/internal/auth"
	"xfeed/server/internal/tokenstore"
)

func main() {
	var (
		listen       = flag.String("listen", ":8443", "HTTPS listen address")
		tlsCert      = flag.String("tls-cert", "", "Path to TLS cert")
		tlsKey       = flag.String("tls-key", "", "Path to TLS key")
		dbPath       = flag.String("db", "/var/lib/thefeed/tokens.db", "SQLite token database path")
		tokenTTLDays = flag.Int("token-ttl-days", 30, "Token TTL in days")
		consumeOnIssue = flag.Bool("consume-on-issue", false, "Consume token on first successful exchange")
		rateLimitRPS   = flag.Int("rate-limit-rps", 5, "Rate limit requests per second per IP")
		disableLogs    = flag.Bool("disable-persistent-logs", true, "Disable persistent logs")
	)
	flag.Parse()
	_ = tokenTTLDays
	_ = disableLogs

	tokenHMACSecret := os.Getenv("TOKEN_HMAC_SECRET")
	thefeedMasterKey := os.Getenv("THEFEED_MASTER_KEY")
	adminSecret := os.Getenv("AUTH_ADMIN_SECRET")
	if tokenHMACSecret == "" || thefeedMasterKey == "" {
		log.Fatal("TOKEN_HMAC_SECRET and THEFEED_MASTER_KEY are required")
	}
	if *tlsCert == "" || *tlsKey == "" {
		log.Fatal("--tls-cert and --tls-key are required")
	}

	store, err := tokenstore.Open(*dbPath)
	if err != nil {
		log.Fatalf("open token store: %v", err)
	}
	defer store.Close()

	svc := auth.NewService(store, []byte(tokenHMACSecret), thefeedMasterKey, adminSecret, *consumeOnIssue, *rateLimitRPS)
	server := &http.Server{
		Addr:              *listen,
		Handler:           svc.Handler(),
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	log.Printf("authd listening on %s", *listen)
	log.Fatal(server.ListenAndServeTLS(*tlsCert, *tlsKey))
}
