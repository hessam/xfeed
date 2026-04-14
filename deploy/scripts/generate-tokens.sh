#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DB_PATH="${1:-${ROOT_DIR}/deploy/state/tokens.db}"
COUNT="${2:-10}"
TTL_DAYS="${3:-30}"

if [[ -z "${TOKEN_HMAC_SECRET:-}" ]]; then
  echo "TOKEN_HMAC_SECRET must be exported before running this script."
  exit 1
fi

cd "${ROOT_DIR}/server"
go run ./cmd/tokenctl --db "${DB_PATH}" --count "${COUNT}" --ttl-days "${TTL_DAYS}" --meta "initial-batch"
