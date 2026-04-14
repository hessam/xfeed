# DNS-based social feed system — product requirements

**Status:** Greenfield / pre-development  
**Scale:** MVP · <100 concurrent users  
**Priority:** Security & anonymity of users  
**Date:** April 2026

---

## Legend

| Tag | Meaning |
|-----|---------|
| `MUST` | Required for launch |
| `SHOULD` | Strong recommendation — omit only with documented reason |
| `NICE` | Post-MVP |

---

## 1. Infrastructure & DNS

| ID | Requirement | Priority |
|----|-------------|----------|
| INF-01 | Deploy TheFeed server on a Hetzner VPS located outside the censored region. | MUST |
| INF-02 | Configure an A record pointing a subdomain (e.g. `ns-server.example.com`) to the Hetzner VPS IP. | MUST |
| INF-03 | Configure an NS record delegating a tunneling subdomain (e.g. `t.example.com`) to the A record above so all encoded queries route directly to the VPS. | MUST |
| INF-04 | Use Cloudflare for the authoritative DNS zone to benefit from DDoS protection and TTL management. | MUST |
| INF-05 | Verify DNS delegation with `dig @vps-ip t.example.com` returning a valid response before any app deployment proceeds. | MUST |
| INF-06 | The VPS firewall must block all inbound traffic except UDP/53, UDP/5300 (localhost only), and SSH. All other ports closed by default. | MUST |

---

## 2. Server — TheFeed + Slipgate

| ID | Requirement | Priority |
|----|-------------|----------|
| SRV-01 | Run TheFeed server in `--no-telegram` public mode for MVP. MTProto / private channel support is out of scope. | MUST |
| SRV-02 | Support both Telegram public channels and X (Twitter) public feeds as content sources. | MUST |
| SRV-03 | TheFeed server must listen on UDP port 5300 and bind only to localhost. Slipgate handles all external traffic forwarding. | MUST |
| SRV-04 | Configure Slipgate with External Routing: match domain `t.example.com`, forward to port 5300. | MUST |
| SRV-05 | All content must be encrypted with AES-256 before encoding into DNS TXT records. | MUST |
| SRV-06 | The AES-256 encryption key must be stored as an environment variable, never hardcoded in source or committed to version control. | MUST |
| SRV-07 | Server must use `deflate` message compression to minimise the number of DNS queries needed per post. | MUST |
| SRV-08 | Random padding must be added to DNS packets to prevent traffic fingerprinting by packet-size analysis (DPI). | MUST |
| SRV-09 | Define and document a manual AES-256 key rotation runbook. Steps must be executable without redeployment. Automated rotation is out of scope for MVP. | SHOULD |

---

## 3. Client — Web UI

| ID | Requirement | Priority |
|----|-------------|----------|
| CLI-01 | Deploy the Web UI as a static frontend on Vercel. The Vercel deployment must contain no server-side Go binaries or persistent UDP socket listeners. | MUST |
| CLI-02 | The client must decode DNS TXT responses and render them as a readable feed without requiring the user to install any native app. | MUST |
| CLI-03 | Content refresh is on-demand (manual pull) only. No automatic polling or background refresh. | MUST |
| CLI-04 | Access must be gated by invite-only token. The UI must not load or display content without a valid, non-expired token. | MUST |
| CLI-05 | Scatter Mode must be active: the client queries multiple resolvers (Cloudflare 1.1.1.1, Google 8.8.8.8) simultaneously to bypass local DNS poisoning. | MUST |
| CLI-06 | The client must track resolver latency per session and automatically prioritise the fastest/most reliable resolver. | SHOULD |
| CLI-07 | Environment variables (`NEXT_PUBLIC_DEFAULT_DOMAIN`, etc.) must be injected at build time via Vercel's environment config, never committed to the repo. | MUST |
| CLI-08 | The UI entry point must be a standard HTTPS URL, blending with regular web traffic. No non-standard ports or protocols are exposed to the end user. | MUST |

---

## 4. Security & anonymity

### 4.1 AES key exposure in client bundle

**Risk:** `NEXT_PUBLIC_DEFAULT_KEY` injected into a Next.js build is visible in the client-side JS bundle to anyone who can access the Vercel URL.

| ID | Requirement | Priority |
|----|-------------|----------|
| SEC-01 | Implement a token-exchange flow: the client uses the invite token to request the AES session key from a lightweight auth endpoint on the VPS, rather than receiving it via an environment variable baked into the bundle. | MUST |
| SEC-02 | The AES passphrase must never appear in the compiled JS bundle at runtime. Verify this post-build with a bundle inspection step in CI. | MUST |
| SEC-03 | The auth endpoint (key exchange) must use HTTPS and validate the invite token before returning any key material. Rate-limit this endpoint to mitigate brute-force attempts. | MUST |

### 4.2 Invite token revocation

**Risk:** A compromised invite token cannot be revoked without redeployment if tokens are static environment variables.

| ID | Requirement | Priority |
|----|-------------|----------|
| SEC-04 | Implement a server-side token allowlist (e.g. a flat file or lightweight KV store on the VPS). The auth endpoint checks this list on every key-exchange request. | MUST |
| SEC-05 | Tokens must be revocable at runtime by removing them from the allowlist without redeploying the Vercel frontend or the TheFeed server binary. | MUST |
| SEC-06 | Tokens must be time-limited. Default expiry should be configurable (suggested: 30 days). Expired tokens must be rejected at the auth endpoint. | MUST |
| SEC-07 | Tokens must be single-use for the key-exchange step. After a session key is issued, the invite token is consumed and cannot be reused to obtain a new key until re-invited. | SHOULD |

### 4.3 General security requirements

| ID | Requirement | Priority |
|----|-------------|----------|
| SEC-08 | No user PII (IP addresses, query logs, access tokens) may be persisted server-side. Server logs must be ephemeral (e.g. piped to `/dev/null` or disabled entirely in production). | MUST |
| SEC-09 | All HTTPS traffic to the Vercel frontend must use TLS 1.2 minimum. Cloudflare's "Full (strict)" SSL mode is required for the domain. | MUST |
| SEC-10 | Produce a threat model document covering: DNS interception, server IP discovery, token leakage, AES key exposure, and Vercel takedown. Each threat must have a defined mitigation or an explicit accepted-risk note. | SHOULD |
| SEC-11 | Provision a backup VPS in a second jurisdiction as a failover, with a documented switchover procedure. | NICE |

---

## 5. Technology stack

| ID | Requirement | Priority |
|----|-------------|----------|
| TCH-01 | Stack is flexible. Evaluate Go for the server (binary efficiency, UDP support) and Next.js/React for the client. Final stack decision must be confirmed before Phase 2 begins. | MUST |
| TCH-02 | Server binary must be buildable as a single self-contained executable for simple VPS deployment with no runtime dependencies. | SHOULD |
| TCH-03 | The client repo must isolate the `/client` or `/web` directory so Vercel only builds the frontend. No server-side code may be included in the Vercel build output. | MUST |
| TCH-04 | The lightweight auth/key-exchange endpoint (SEC-01) must be deployed on the Hetzner VPS, not on Vercel. It requires a persistent HTTPS listener. | MUST |

---

## 6. Development & testing phases

| ID | Phase | Acceptance criteria | Priority |
|----|-------|---------------------|----------|
| DEV-01 | Phase 1 — DNS | `dig @vps-ip t.example.com` returns a valid response. | MUST |
| DEV-02 | Phase 2 — Slipgate | UDP traffic on port 53 is forwarded to a dummy listener on port 5300. | MUST |
| DEV-03 | Phase 3 — TheFeed server | A public Telegram channel and a public X feed return readable posts via DNS. | MUST |
| DEV-04 | Phase 4 — Auth endpoint | Token-exchange flow issues a session key; invalid/expired tokens are rejected. | MUST |
| DEV-05 | Phase 5 — Vercel client | End-to-end test from a restricted network simulator (VPN or local DNS-blocked environment) with invite token flow working. | MUST |
| DEV-06 | All phases | Each phase must have a documented rollback procedure before it is started. | SHOULD |
| DEV-07 | Phase 5 | Post-build bundle inspection confirms the AES key does not appear in the compiled JS. | MUST |

---

## 7. Out of scope for MVP

| ID | Item |
|----|------|
| OOS-01 | MTProto / private Telegram channels (requires API_ID/API_HASH). Revisit post-MVP. |
| OOS-02 | Automated AES-256 key rotation. Manual runbook (SRV-09) is sufficient for pilot. |
| OOS-03 | Uptime monitoring and alerting. No tooling planned for MVP. |
| OOS-04 | User accounts or persistent session management. Token-based invite is the only auth layer. |
| OOS-05 | Multi-VPS geographic redundancy (covered by SEC-11 as a post-MVP nice-to-have). |
| OOS-06 | Content caching or offline reading. |

---

## 8. Resolved decisions

| # | Decision | Resolution | Rationale |
|---|----------|------------|-----------|
| D-01 | Stack selection | **Go (server) + Next.js (client)** | Go: single binary, native UDP, low VPS memory footprint. Next.js: existing repo is React-based, no rewrite needed for MVP. |
| D-02 | Token allowlist storage | **SQLite on VPS** | Flat file is too fragile under concurrent revocations. Redis is overkill and an additional attack surface. SQLite is zero-config, fast enough for <100 users, and trivially upgradeable to Postgres/Redis if scale demands it. |
| D-03 | AES key rotation interval | **30 days, manual runbook** | Aligned with token expiry (D-04) so both rotate together, simplifying ops. Short enough to limit blast radius; long enough that manual rotation is not a weekly burden for a pilot. |
| D-04 | Invite token expiry | **30 days** | Matches key rotation window. Simple to communicate to invited users. |
| D-05 | MTProto (private channels) scope | **Target v1.1 post-pilot** | If the pilot proves demand, private channel access is the most natural next feature. Flag in backlog now so server auth architecture does not actively block it later. |
