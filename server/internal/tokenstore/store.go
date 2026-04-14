package tokenstore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrTokenNotFound = errors.New("token not found")
	ErrTokenExpired  = errors.New("token expired")
	ErrTokenRevoked  = errors.New("token revoked")
	ErrTokenConsumed = errors.New("token consumed")
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) ValidateAndConsume(ctx context.Context, tokenHash []byte, consume bool) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var expiresAt int64
	var revoked, consumed int
	err = tx.QueryRowContext(ctx, `
		SELECT expires_at, revoked, consumed
		FROM token_allowlist
		WHERE token_hash = ?
		LIMIT 1
	`, tokenHash).Scan(&expiresAt, &revoked, &consumed)
	if err == sql.ErrNoRows {
		return ErrTokenNotFound
	}
	if err != nil {
		return err
	}

	now := time.Now().UTC().Unix()
	if revoked == 1 {
		return ErrTokenRevoked
	}
	if expiresAt < now {
		return ErrTokenExpired
	}
	if consumed == 1 {
		return ErrTokenConsumed
	}

	if consume {
		if _, err := tx.ExecContext(ctx, `
			UPDATE token_allowlist
			SET consumed = 1
			WHERE token_hash = ? AND consumed = 0
		`, tokenHash); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Insert(ctx context.Context, tokenHash []byte, issuedAt, expiresAt int64, metadata string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO token_allowlist
		(token_hash, issued_at, expires_at, consumed, revoked, metadata)
		VALUES (?, ?, ?, 0, 0, ?)
	`, tokenHash, issuedAt, expiresAt, metadata)
	return err
}
