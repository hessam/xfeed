# Project charter — DNS social feed system

**Version:** 1.0  
**Date:** April 2026  
**Status:** Approved for development

---

## 1. Purpose

Provide general public users in censored regions with reliable, anonymous access to Telegram public channels and X (Twitter) public feeds — using DNS tunneling to bypass network-level filtering where standard internet access is blocked.

---

## 2. Problem statement

In heavily restricted network environments, social media platforms are blocked at the DNS and IP layer. Standard VPNs are increasingly detected and throttled. DNS traffic (UDP/53) is among the last reliably open channels. This project exploits that opening to deliver read-only social feeds through encrypted DNS TXT records, with no app installation required by the end user.

---

## 3. Goals

| # | Goal |
|---|------|
| G-01 | Deliver a working read-only feed for Telegram public channels and X public posts via DNS tunneling. |
| G-02 | Ensure user anonymity — no PII logged or persisted at any layer. |
| G-03 | Keep the user entry point indistinguishable from ordinary HTTPS web traffic. |
| G-04 | Complete a pilot with fewer than 100 concurrent users before scaling. |
| G-05 | Build the architecture so private channel support (MTProto) can be added in v1.1 without a rewrite. |

---

## 4. Out of scope (MVP)

- Private Telegram channels (MTProto / API credentials)
- Automated key rotation
- Uptime monitoring and alerting
- User accounts or persistent sessions
- Multi-region VPS redundancy
- Content caching or offline reading

---

## 5. Deliverables

| Deliverable | Description |
|-------------|-------------|
| Hetzner VPS | Configured server running Slipgate + TheFeed Go binary + Auth service |
| DNS zone | Cloudflare-managed zone with A + NS records delegating `t.example.com` to VPS |
| Auth service | Lightweight Go HTTPS endpoint for token validation and AES session key exchange |
| Token store | SQLite database on VPS managing invite tokens (issuance, expiry, revocation) |
| Web UI | Next.js app deployed on Vercel — static, HTTPS, invite-token gated |
| Key rotation runbook | Documented manual procedure for rotating AES-256 keys every 30 days |
| Threat model | Document covering DNS interception, IP discovery, token leakage, key exposure, and takedown scenarios |

---

## 6. Technology stack

| Layer | Technology | Rationale |
|-------|------------|-----------|
| Server runtime | Go | Single binary, native UDP, minimal VPS footprint |
| Traffic router | Slipgate | External routing of DNS traffic to TheFeed port |
| Feed server | TheFeed (`--no-telegram` mode) | Purpose-built DNS feed server, public mode for MVP |
| Auth / key exchange | Go HTTPS service on VPS | Persistent listener; cannot live on Vercel |
| Token store | SQLite on VPS | Zero-config, zero dependencies, upgradeable |
| Web client | Next.js (React) | Existing codebase, static export, Vercel-compatible |
| CDN / DNS protection | Cloudflare | DDoS protection, TTL control, TLS strict mode |
| Hosting (UI) | Vercel | Static frontend only — no server-side code |

---

## 7. Security principles

1. **No key in the bundle.** The AES-256 session key is never baked into the client JS. Users exchange their invite token for a session key via a server-side HTTPS endpoint.
2. **No logs.** Server-side logging is disabled in production. No IP addresses, tokens, or query data are persisted.
3. **Tokens expire and are revocable.** All invite tokens have a 30-day TTL and can be invalidated at runtime without redeployment.
4. **Traffic blends in.** The user-facing entry point is a plain HTTPS URL. DNS queries use scatter mode across multiple public resolvers and include random padding to defeat DPI fingerprinting.
5. **Rotate together.** AES keys and token expiry are both 30-day cycles, kept in sync to simplify the ops runbook.

---

## 8. Development phases

| Phase | Work | Gate to proceed |
|-------|------|-----------------|
| 1 — DNS | Configure A + NS records on Cloudflare | `dig @vps-ip t.example.com` returns response |
| 2 — Routing | Install Slipgate, configure external route to port 5300 | UDP forwarded to dummy listener on 5300 |
| 3 — Feed server | Deploy TheFeed Go binary, test Telegram + X public feeds | Readable posts returned via DNS |
| 4 — Auth | Deploy token exchange endpoint, wire SQLite token store | Valid tokens issue key; invalid/expired tokens rejected |
| 5 — Client | Deploy Next.js UI on Vercel, end-to-end test from restricted network | Full flow works; AES key absent from JS bundle |

Each phase requires a documented rollback procedure before it begins.

---

## 9. Constraints & assumptions

- The VPS must be hosted in a jurisdiction outside the target censored region.
- Vercel is used for the UI only. It cannot host Go binaries or persistent UDP/TCP listeners.
- The system relies on UDP/53 being open in the target network. If that port is also blocked, the system cannot function.
- X (Twitter) public feed access depends on continued availability of a scraping or public API method — this may require ongoing maintenance.
- MTProto support (private channels) is deferred to v1.1. The auth architecture must not foreclose this option.

---

## 10. Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| UDP/53 blocked in target network | Medium | Critical | Scatter mode across resolvers; no workaround if all DNS is blocked |
| VPS IP discovered and blocked | Medium | High | Manual switchover runbook; backup VPS in v1.1 |
| Vercel takedown of UI | Low | Medium | UI is static and can be redeployed to any CDN in minutes |
| AES key leaked via bundle | Low (mitigated) | High | Token-exchange flow (SEC-01–03) prevents key appearing in bundle |
| Invite token compromised | Low (mitigated) | Medium | SQLite allowlist + 30-day expiry + runtime revocation (SEC-04–07) |
| X API / scraping breaks | Medium | Medium | Degrade gracefully to Telegram-only; fix in a patch release |
