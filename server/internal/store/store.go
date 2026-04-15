package store

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Post mirrors feed.Item for storage.
type Post struct {
	Source  string
	Text    string
	PubDate string
}

// Store is a SQLite-backed post store.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite DB at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite is single-writer
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS posts (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  source     TEXT NOT NULL,
  text       TEXT NOT NULL,
  pub_date   TEXT NOT NULL,
  fetched_at TEXT NOT NULL,
  hash       TEXT NOT NULL UNIQUE
);
CREATE TABLE IF NOT EXISTS cursors (
  source    TEXT PRIMARY KEY,
  last_seen TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_posts_pub_date ON posts(pub_date DESC);
`)
	return err
}

// InsertBatch inserts posts, ignoring duplicates.
// Returns number of new rows inserted.
func (s *Store) InsertBatch(posts []Post) (int, error) {
	if len(posts) == 0 {
		return 0, nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO posts(source,text,pub_date,fetched_at,hash) VALUES(?,?,?,?,?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	inserted := 0
	for _, p := range posts {
		h := hashPost(p.Source, p.Text)
		res, err := stmt.Exec(p.Source, p.Text, p.PubDate, now, h)
		if err != nil {
			continue
		}
		n, _ := res.RowsAffected()
		inserted += int(n)
	}
	return inserted, tx.Commit()
}

// Latest returns the N most recent posts.
func (s *Store) Latest(limit int) ([]Post, error) {
	rows, err := s.db.Query(`
SELECT source, text, pub_date FROM posts
ORDER BY pub_date DESC, id DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Post
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.Source, &p.Text, &p.PubDate); err != nil {
			continue
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetCursor returns the last_seen timestamp for source, or "" if none.
func (s *Store) GetCursor(source string) string {
	var ts string
	_ = s.db.QueryRow(`SELECT last_seen FROM cursors WHERE source=?`, source).Scan(&ts)
	return ts
}

// SetCursor upserts the last_seen timestamp for source.
func (s *Store) SetCursor(source, lastSeen string) error {
	_, err := s.db.Exec(`INSERT INTO cursors(source,last_seen) VALUES(?,?)
ON CONFLICT(source) DO UPDATE SET last_seen=excluded.last_seen`, source, lastSeen)
	return err
}

// Prune removes posts older than the specified duration (relative to current time).
func (s *Store) Prune(maxAge time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-maxAge).Format(time.RFC3339)
	res, err := s.db.Exec(`DELETE FROM posts WHERE fetched_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func hashPost(source, text string) string {
	h := sha256.Sum256([]byte(source + "\x00" + text))
	return fmt.Sprintf("%x", h)
}
