#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DEPLOY_DIR="${ROOT_DIR}/deploy"
STATE_DIR="${DEPLOY_DIR}/state"
TLS_DIR="${DEPLOY_DIR}/tls"
DB_PATH="${STATE_DIR}/tokens.db"
SMOKE_TOKEN="${SMOKE_TOKEN:-smoke-invite-token-001}"
TUNNEL_DOMAIN="${THEFEED_DOMAIN:-t.example.com}"
export DEPLOY_DIR DB_PATH SMOKE_TOKEN

mkdir -p "${STATE_DIR}" "${TLS_DIR}"

if [[ ! -f "${DEPLOY_DIR}/.env" ]]; then
  cp "${DEPLOY_DIR}/.env.example" "${DEPLOY_DIR}/.env"
fi

if ! awk '/^TOKEN_HMAC_SECRET=/{found=1} END{exit !found}' "${DEPLOY_DIR}/.env"; then
  echo "TOKEN_HMAC_SECRET=smoke-test-hmac-secret-change-me" >> "${DEPLOY_DIR}/.env"
fi
if ! awk '/^THEFEED_MASTER_KEY=/{found=1} END{exit !found}' "${DEPLOY_DIR}/.env"; then
  echo "THEFEED_MASTER_KEY=smoke-test-master-key-change-me" >> "${DEPLOY_DIR}/.env"
fi
if ! awk '/^THEFEED_DOMAIN=/{found=1} END{exit !found}' "${DEPLOY_DIR}/.env"; then
  echo "THEFEED_DOMAIN=${TUNNEL_DOMAIN}" >> "${DEPLOY_DIR}/.env"
fi

if [[ ! -f "${TLS_DIR}/fullchain.pem" || ! -f "${TLS_DIR}/privkey.pem" ]]; then
  openssl req -x509 -nodes -newkey rsa:2048 \
    -keyout "${TLS_DIR}/privkey.pem" \
    -out "${TLS_DIR}/fullchain.pem" \
    -days 3 \
    -subj "/CN=authd/O=TheFeed Smoke Test"
fi

python3 - <<'PY'
import hmac
import hashlib
import os
import sqlite3
import time

deploy_dir = os.environ["DEPLOY_DIR"]
db_path = os.environ["DB_PATH"]
smoke_token = os.environ["SMOKE_TOKEN"]

secret = ""
with open(os.path.join(deploy_dir, ".env"), "r", encoding="utf-8") as f:
    for line in f:
        if line.startswith("TOKEN_HMAC_SECRET="):
            secret = line.split("=", 1)[1].strip()
            break
if not secret:
    raise RuntimeError("TOKEN_HMAC_SECRET not found in deploy/.env")

token_hash = hmac.new(secret.encode(), smoke_token.encode(), hashlib.sha256).digest()
now = int(time.time())
expires = now + 86400

conn = sqlite3.connect(db_path)
with open(os.path.join(os.path.dirname(deploy_dir), "server", "internal", "tokenstore", "schema.sql"), "r", encoding="utf-8") as f:
    conn.executescript(f.read())
conn.execute(
    """
    INSERT OR REPLACE INTO token_allowlist (token_hash, issued_at, expires_at, consumed, revoked, metadata)
    VALUES (?, ?, ?, 0, 0, ?)
    """,
    (token_hash, now, expires, '{"purpose":"smoke-test"}')
)
conn.commit()
conn.close()
PY

echo "Starting isolated smoke stack..."
SLIPGATE_BIND_IP=127.0.0.1 \
SLIPGATE_HOST_PORT=1053 \
AUTH_BIND_IP=127.0.0.1 \
AUTH_HOST_PORT=18443 \
docker compose -f "${DEPLOY_DIR}/docker-compose.yml" -f "${DEPLOY_DIR}/docker-compose.smoke.yml" up -d --build

cleanup() {
  SLIPGATE_BIND_IP=127.0.0.1 \
  SLIPGATE_HOST_PORT=1053 \
  AUTH_BIND_IP=127.0.0.1 \
  AUTH_HOST_PORT=18443 \
  docker compose -f "${DEPLOY_DIR}/docker-compose.yml" -f "${DEPLOY_DIR}/docker-compose.smoke.yml" down -v
}
trap cleanup EXIT

echo "[1/3] Slipgate handover: host via 1053 and direct thefeed via 5300"
dig @127.0.0.1 -p 1053 "smoke.${TUNNEL_DOMAIN}" TXT +short
for attempt in 1 2 3; do
  if docker run --rm --network feed_net -e TUNNEL_DOMAIN="${TUNNEL_DOMAIN}" python:3.12-alpine \
    python - <<'PY'
import os
import random
import socket
import struct

domain = "smoke." + os.environ["TUNNEL_DOMAIN"].strip(".")
qname = b"".join(bytes([len(x)]) + x.encode() for x in domain.split(".")) + b"\x00"
qid = random.randint(0, 65535)
query = struct.pack("!HHHHHH", qid, 0x0100, 1, 0, 0, 0) + qname + struct.pack("!HH", 16, 1)

sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
sock.settimeout(2.5)
sock.sendto(query, ("thefeed", 5300))
data, _ = sock.recvfrom(2048)
if len(data) < 12:
    raise SystemExit(1)
print("direct thefeed response bytes:", len(data))
PY
  then
    break
  fi
  if [[ "${attempt}" == "3" ]]; then
    echo "ERROR: direct thefeed UDP/5300 query failed after retries"
    exit 1
  fi
  sleep 1
done

echo "[2/3] Auth path from simulated client container"
docker run --rm --network feed_net curlimages/curl:8.12.1 \
  -sk -X POST "https://authd:8443/v1/token/exchange" \
  -H "content-type: application/json" \
  -d "{\"invite_token\":\"${SMOKE_TOKEN}\"}"

echo "[3/3] Isolation check: bindings must be loopback only in smoke mode"
docker ps --filter "name=thefeed-" --format "{{.Names}} {{.Ports}}"
if docker ps --filter "name=thefeed-" --format "{{.Ports}}" | awk '/0\.0\.0\.0:/{exit 0} END{exit 1}'; then
  echo "ERROR: found 0.0.0.0 bindings in smoke mode"
  exit 1
fi

echo "Smoke test passed."
