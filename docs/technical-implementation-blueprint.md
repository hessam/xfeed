# TheFeed Technical Implementation Blueprint

## 1) Monorepo Scaffolding

```text
Xfeed/
  docs/
    technical-implementation-blueprint.md
  server/                      # Go services deployed to VPS
    cmd/
      thefeed/                 # DNS feed binary entrypoint
      authd/                   # token-exchange HTTPS entrypoint (SEC-01)
    internal/
      dns/
      feed/
      crypto/
      auth/
      tokenstore/
        schema.sql             # SQLite allowlist schema (D-02)
      ratelimit/
  web/                         # Next.js frontend for Vercel only
    src/
    public/
    vercel.json
  deploy/                      # isolated deployment artifacts
    docker-compose.yml
    .env.example
    slipgate/
      slipgate.conf
    thefeed/
      Dockerfile
    scripts/
      setup.sh
```

Design intent:
- Keep VPS runtime code (`server`) physically separate from Vercel runtime code (`web`) to satisfy `TCH-03`, `CLI-01`, and `TCH-04`.
- Keep all host-impacting operations in containers under `deploy/`; no global host package installs and no host-level firewall mutation in scripts.

---

## 2) Implementation Roadmap (Phased)

## Phase 1: DNS + Networking (`INF-01`..`INF-05`, `DEV-01`)
- Configure Cloudflare records:
  - `A ns-server.example.com -> VPS_PUBLIC_IP` (`INF-02`)
  - `NS t.example.com -> ns-server.example.com` (`INF-03`)
- Run `deploy/scripts/setup.sh` to validate:
  - authoritative NS delegation,
  - direct response from VPS resolver path (`dig @VPS_IP t.example.com`),
  - TXT query path sanity for tunnel domain (`INF-05`).
- Gate: No Phase 2 until all checks return `NOERROR`.

Rollback:
- Revert NS delegation to previous value in Cloudflare.
- Keep old endpoint alive until TTL expiry.

## Phase 2: Isolated Server Deployment (`SRV-03`, `SRV-04`, `DEV-02`)
- Launch `deploy/docker-compose.yml` with a dedicated bridge network `thefeed_net` (internal).
- Services:
  - `slipgate` binds `53/udp` to host.
  - `thefeed` binds only inside compose network (`5300/udp` internal).
  - `authd` binds `8443/tcp` for token exchange over TLS behind reverse proxy or direct.
- No global iptables rules; only Docker port mapping for explicit ports.
- Gate: UDP/53 traffic reaches TheFeed through Slipgate route.

Rollback:
- `docker compose down` and restore previous compose revision.

## Phase 3: Security + Key Exchange (`SEC-01`..`SEC-08`, `DEV-04`, `DEV-07`)
- Implement `POST /v1/token/exchange` on `authd`:
  1. Parse invite token.
  2. Hash token using `HMAC-SHA256(server_secret, token)`.
  3. Lookup hash in SQLite allowlist (indexed lookup).
  4. Enforce: exists, not revoked, not expired, and (if enabled) not consumed.
  5. Apply per-IP + per-token hash rate limits.
  6. If valid:
     - issue short-lived encrypted session blob (or direct session key envelope),
     - mark token consumed (`SEC-07`, SHOULD),
     - return key material only over HTTPS (`SEC-03`).
  7. For invalid path: always return generic `401` with constant-time behavior.
- Ensure production logs are disabled/minimal and do not persist PII (`SEC-08`).
- CI post-build grep/scan of web bundle to verify AES material absence (`SEC-02`).

Rollback:
- Revoke all untrusted tokens in SQLite and rotate `THEFEED_MASTER_KEY`.

## Phase 4: Frontend Deployment (`CLI-01`..`CLI-08`, `DEV-05`)
- Build static Next.js export on Vercel.
- Required env:
  - `NEXT_PUBLIC_DEFAULT_DOMAIN=t.example.com`
  - `NEXT_PUBLIC_AUTH_BASE_URL=https://auth.example.com`
- Client flow:
  - token input -> HTTPS exchange endpoint -> in-memory session key -> DNS fetch/decrypt/render.
- Enable Scatter Mode to query both resolvers and pick lowest-latency responder (`CLI-05`, `CLI-06`).

Rollback:
- Re-deploy last known-good Vercel build; revoke compromised tokens server-side.

---

## 3) Critical Security Verification

Pre-deployment checklist for Slipgate <-> TheFeed external routing:
- `Route Match`: `t.example.com` wildcard route exists and only forwards to `thefeed:5300/udp`.
- `Port Exposure`: only host `53/udp` and auth HTTPS port are published.
- `Binding`: TheFeed does not publish host ports and listens on `0.0.0.0:5300` inside private network only.
- `Packet Budget`: max DNS TXT payload chunking keeps each response under 512 bytes wire size (header + question + answer + padding).
- `Padding`: random padding range configured and bounded so padded packets remain below MTU target.
- `Compression`: `deflate` enabled before encryption and encoding.
- `TLS`: auth endpoint presents valid cert chain; TLS >= 1.2.
- `Rate Limit`: token exchange enforces burst + sustained limits.
- `No PII persistence`: request logs scrubbed/disabled; no token plaintext storage.
- `Fail-Closed`: routing or token DB failure returns deny/no-data, never fallback plaintext key.

SQLite allowlist design (`D-02`) for high-performance lookups:
- Store only token hash bytes (`BLOB`) not plaintext tokens.
- Covering index for hot validation path:
  - `(token_hash, revoked, consumed, expires_at)`.
- WAL mode + `synchronous=NORMAL` for fast concurrent read-heavy access.
- Prepared statements reused by auth process.
- UTC integer timestamps for cheap comparisons.

Validation query pattern:
```sql
SELECT token_hash, expires_at, revoked, consumed
FROM token_allowlist
WHERE token_hash = ?1
LIMIT 1;
```

---

## 4) Initial Deployment Artifacts Included

- `deploy/slipgate/slipgate.conf` (External Routing template for `t.example.com` -> `thefeed:5300`)
- `deploy/thefeed/Dockerfile` (Go multi-stage minimal runtime)
- `deploy/scripts/setup.sh` (`INF-05` DNS health checks with `dig`)
- `deploy/docker-compose.yml` (isolated private network deployment)
- `deploy/.env.example` (runtime variables without secrets committed)
- `server/internal/tokenstore/schema.sql` (SQLite allowlist schema for `SEC-04`..`SEC-07`)

---

## 5) Hetzner Cutover Hardening Notes

- Use dual-network compose model:
  - `feed_net` (`internal: true`) for east-west service traffic (`slipgate` <-> `thefeed` <-> `authd`).
  - `public_net` for explicitly published ingress only.
- Put TLS termination on existing reverse proxy (`nginx`/`traefik`) and proxy to `authd` container endpoint.
- Ensure host ownership for `deploy/state` is UID/GID `65532:65532` before go-live to match distroless `nonroot`.
- Keep `thefeed` unexposed on host ports in production (`expose` only) and publish only:
  - UDP/53 for Slipgate
  - TLS ingress path for auth endpoint via reverse proxy.
