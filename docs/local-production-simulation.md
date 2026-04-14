# Local Production Simulation

This runbook validates the full path locally with real internet data:

1. `authd` token exchange + envelope unwrap (PBKDF2 + AES-GCM)
2. `slipgate` -> `thefeed` DNS handover
3. multipart TXT chunk assembly in the browser shell
4. decrypt + inflate + render flow

## 1) Start backend stack (internet enabled)

```bash
SLIPGATE_BIND_IP=127.0.0.1 \
SLIPGATE_HOST_PORT=1053 \
AUTH_BIND_IP=127.0.0.1 \
AUTH_HOST_PORT=18443 \
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.smoke.yml up -d
```

Ensure `deploy/state/channels.txt` contains at least one public channel (already seeded with `durov`).

## 2) Trigger and verify DNS pipeline

```bash
dig @127.0.0.1 -p 1053 p1.msg.t.example.com TXT +short
```

Expected shape:
- TXT answer begins with `c:<part>/<total>:`
- payload is base64url-like ciphertext chunk

## 3) Run web shell in local bridge mode

Set frontend env:

```bash
NEXT_PUBLIC_AUTH_BASE_URL=https://127.0.0.1:18443
NEXT_PUBLIC_DEFAULT_DOMAIN=t.example.com
NEXT_PUBLIC_DNS_BRIDGE_URL=http://localhost:3000/api/local-dns
LOCAL_DNS_SERVER_HOST=127.0.0.1
LOCAL_DNS_SERVER_PORT=1053
```

The local bridge endpoint (`web/src/app/api/local-dns/route.ts`) receives HTTP and forwards UDP DNS queries to local Slipgate.

## 4) Validate telemetry budget (<3 seconds target)

The reference UI overlay shows:
- `T1`: token exchange RTT
- `T2`: PBKDF2 unwrap time
- `T3`: DNS multipart fetch RTT
- `T4`: decrypt + decode + render

Track `T1+T2+T3+T4` and optimize toward `< 3000ms`.

## 5) Teardown

```bash
SLIPGATE_BIND_IP=127.0.0.1 \
SLIPGATE_HOST_PORT=1053 \
AUTH_BIND_IP=127.0.0.1 \
AUTH_HOST_PORT=18443 \
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.smoke.yml down
```
