# System architecture — DNS social feed system

**Version:** 1.0  
**Date:** April 2026

---

## Overview

The system is split across three zones. The user never interacts with anything that looks like a custom protocol — they visit an HTTPS URL, authenticate with a token, and receive a feed decoded silently in the browser from DNS responses. The VPS is the only component that handles sensitive data or persistent state.

```
[ Browser + Vercel ]  →  [ Cloudflare edge ]  →  [ Hetzner VPS ]
      User zone               DNS + TLS             Server zone
```

---

## Zone 1 — User side

### Browser (Next.js web UI)
- Served as a static export from Vercel over HTTPS.
- On load, presents an invite-token input field.
- Exchanges the invite token for an AES session key via the Auth service on the VPS (HTTPS, rate-limited).
- Once a session key is issued, performs on-demand DNS queries using Scatter Mode — querying Cloudflare `1.1.1.1` and Google `8.8.8.8` simultaneously.
- Tracks resolver latency per session and routes subsequent queries to the fastest responder.
- Decodes and decrypts DNS TXT responses client-side using the AES session key.
- Renders the decoded posts as a read-only feed.
- The AES key never appears in the compiled JS bundle. Verified post-build by a CI bundle inspection step.

### Vercel
- Hosts the static Next.js frontend only.
- No server-side Go code, no UDP listeners.
- Environment variables (`NEXT_PUBLIC_DEFAULT_DOMAIN`) injected at build time — never committed to the repo.
- Entry point is a standard HTTPS URL, indistinguishable from any other website.

---

## Zone 2 — Cloudflare edge

### DNS zone
- Authoritative zone for the domain managed on Cloudflare.
- A record: `ns-server.example.com` → Hetzner VPS IP.
- NS record: `t.example.com` delegated to `ns-server.example.com`.
- Effect: any DNS query for `[encoded-data].t.example.com` is routed directly to the VPS.

### TLS + DDoS layer
- Cloudflare "Full (strict)" SSL mode enforced on the domain.
- Provides DDoS absorption for the Vercel frontend and the DNS edge.
- Does not terminate DNS traffic — UDP/53 is passed through to the VPS via the NS delegation.

---

## Zone 3 — Hetzner VPS

### Firewall
- Inbound: UDP/53 (public), TCP/443 (Auth service HTTPS), SSH only.
- All other ports closed. UDP/5300 bound to localhost only.

### Slipgate
- Listens on UDP/53.
- External Routing rule: match domain `t.example.com`, forward to localhost:5300.
- Acts as the traffic broker between the public DNS port and the TheFeed server.

### TheFeed server (Go binary)
- Listens on UDP/5300, localhost only.
- Runs in `--no-telegram` public mode for MVP.
- Fetches posts from Telegram public channels and X (Twitter) public feeds.
- Compresses content with `deflate` before encoding.
- Encrypts encoded content with AES-256 using a key stored as an environment variable.
- Adds random byte padding to DNS packets to defeat DPI fingerprinting.
- Responds to DNS TXT queries with encrypted, compressed, padded payload.

### Auth service (Go HTTPS binary)
- Persistent HTTPS listener on the VPS (cannot run on Vercel).
- Receives invite token from the browser client.
- Validates token against the SQLite allowlist: checks existence, expiry (30-day TTL), and revocation status.
- On valid token: issues a one-time AES session key, marks the invite token as consumed.
- On invalid/expired/revoked token: returns 401, logs nothing.
- Rate-limited to mitigate brute-force attempts.

### SQLite token store
- Single-file database on the VPS.
- Schema: token (hash), issued_at, expires_at, consumed (bool), revoked (bool).
- Tokens can be inserted, expired, and revoked at runtime via a simple CLI tool — no redeployment required.
- Upgradeable to Postgres or Redis if multi-VPS redundancy is added in a future version.

---

## Data flows

### 1. First load — token exchange
```
Browser → Vercel HTTPS       : fetches static UI
Browser → Auth service HTTPS : POST { invite_token }
Auth    → SQLite             : validate token (exists, not expired, not revoked)
SQLite  → Auth               : OK
Auth    → Browser            : { aes_session_key }  (token marked consumed)
```

### 2. Feed request — DNS tunnel
```
Browser → Cloudflare 1.1.1.1 (+ Google 8.8.8.8 in parallel, scatter mode)
Cloudflare → VPS UDP/53      : DNS query for [encoded-request].t.example.com
Slipgate → TheFeed UDP/5300  : forwards query
TheFeed  → Telegram / X API  : fetches posts
TheFeed  → Slipgate          : DNS TXT response (AES-256 encrypted, deflate, random padding)
Slipgate → Cloudflare        : returns DNS TXT
Cloudflare → Browser         : response
Browser  → Browser           : decrypt AES, decompress deflate, render feed
```

---

## Security controls summary

| Control | Where applied | Mechanism |
|---------|---------------|-----------|
| AES-256 encryption | TheFeed server | Content encrypted before DNS encoding |
| Key never in bundle | Browser / CI | Token-exchange flow; CI bundle inspection gate |
| Token expiry | Auth + SQLite | 30-day TTL, enforced at validation time |
| Token revocation | SQLite | Runtime CLI tool, no redeployment needed |
| No PII logging | VPS | Logging disabled in production |
| DPI resistance | TheFeed server | Random packet padding + deflate compression |
| DNS poisoning bypass | Browser | Scatter mode across multiple public resolvers |
| TLS enforcement | Cloudflare | Full strict mode, TLS 1.2 minimum |
| Firewall | VPS | Only UDP/53, HTTPS, SSH inbound |

---

## Upgrade paths (post-MVP)

| Capability | What changes |
|------------|-------------|
| MTProto (private channels) | Add API_ID/API_HASH to TheFeed server config; Auth architecture unchanged |
| Automated key rotation | Add a cron job on the VPS; no client changes needed |
| Multi-VPS redundancy | Replace SQLite with Postgres; add DNS round-robin or failover record |
| Uptime monitoring | Add UptimeRobot or similar pointing at the Auth service HTTPS endpoint |
| Private channel access tiers | Extend SQLite token schema with a `tier` column |
