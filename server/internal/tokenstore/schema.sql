PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA temp_store = MEMORY;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS token_allowlist (
  token_hash BLOB PRIMARY KEY,                 -- HMAC-SHA256(secret, token)
  issued_at INTEGER NOT NULL,                  -- unix epoch seconds (UTC)
  expires_at INTEGER NOT NULL,                 -- unix epoch seconds (UTC)
  consumed INTEGER NOT NULL DEFAULT 0 CHECK (consumed IN (0, 1)),
  revoked INTEGER NOT NULL DEFAULT 0 CHECK (revoked IN (0, 1)),
  revoked_at INTEGER,
  metadata TEXT
);

-- Hot path index for SEC-04..SEC-07 validation checks.
CREATE INDEX IF NOT EXISTS idx_token_allowlist_validate
ON token_allowlist (token_hash, revoked, consumed, expires_at);

-- Optional maintenance index for cleaning up stale rows.
CREATE INDEX IF NOT EXISTS idx_token_allowlist_expiry
ON token_allowlist (expires_at);
