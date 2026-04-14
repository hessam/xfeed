#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DB_PATH="${1:-${ROOT_DIR}/deploy/state/tokens.db}"
SCHEMA_PATH="${ROOT_DIR}/server/internal/tokenstore/schema.sql"

mkdir -p "$(dirname "${DB_PATH}")"
sqlite3 "${DB_PATH}" < "${SCHEMA_PATH}"
echo "Initialized SQLite allowlist at ${DB_PATH}"
