package index

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Token represents a bearer authentication token stored in the index.
type Token struct {
	Hash      string     // SHA-256 hex of the plaintext
	Handle    string     // actor handle
	CreatedAt time.Time  // creation time
	ExpiresAt *time.Time // nil means never expires
	Revoked   bool       // true if the token has been revoked
	Label     string     // optional human-readable label
}

// CreateToken generates a new bearer token for the given handle. ttl=0 means
// no expiry. Returns the plaintext token (shown once) and stores only the hash.
func CreateToken(db *DB, handle, label string, ttl time.Duration) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("index: token: generate random bytes: %w", err)
	}
	plaintext := base64.RawURLEncoding.EncodeToString(raw)

	sum := sha256.Sum256([]byte(plaintext))
	hash := hex.EncodeToString(sum[:])

	now := time.Now().UTC()

	var expiresAt *time.Time
	if ttl != 0 {
		t := now.Add(ttl)
		expiresAt = &t
	}

	var expiresStr any
	if expiresAt != nil {
		expiresStr = expiresAt.Format(time.RFC3339)
	}

	_, err := db.conn.Exec(
		`INSERT INTO tokens (token_hash, handle, created_at, expires_at, revoked, label)
		 VALUES (?, ?, ?, ?, 0, ?)`,
		hash, handle, now.Format(time.RFC3339), expiresStr, nullableString(label),
	)
	if err != nil {
		return "", fmt.Errorf("index: token: insert: %w", err)
	}
	return plaintext, nil
}

// LookupToken looks up a token by its plaintext value. Returns sql.ErrNoRows
// if the token is missing, revoked, or expired.
func LookupToken(db *DB, plaintext string) (*Token, error) {
	sum := sha256.Sum256([]byte(plaintext))
	hash := hex.EncodeToString(sum[:])

	row := db.conn.QueryRow(
		`SELECT token_hash, handle, created_at, expires_at, revoked, label
		 FROM tokens WHERE token_hash = ?`,
		hash,
	)

	tok, err := scanToken(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("index: token: lookup: %w", err)
	}

	if tok.Revoked {
		return nil, sql.ErrNoRows
	}
	if tok.ExpiresAt != nil && time.Now().After(*tok.ExpiresAt) {
		return nil, sql.ErrNoRows
	}

	return tok, nil
}

// RevokeTokenByHashPrefix finds a token whose hash starts with prefix and
// marks it revoked. Returns the full hash on success. Returns an error if
// zero or more than one token matches.
func RevokeTokenByHashPrefix(db *DB, prefix string) (string, error) {
	rows, err := db.conn.Query(
		`SELECT token_hash FROM tokens WHERE token_hash LIKE ? AND revoked = 0`,
		prefix+"%",
	)
	if err != nil {
		return "", fmt.Errorf("index: token: revoke lookup: %w", err)
	}
	defer rows.Close()

	var hashes []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return "", fmt.Errorf("index: token: revoke scan: %w", err)
		}
		hashes = append(hashes, h)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("index: token: revoke rows: %w", err)
	}

	switch len(hashes) {
	case 0:
		return "", fmt.Errorf("index: token: no active token matches prefix %q", prefix)
	case 1:
		// good
	default:
		return "", fmt.Errorf("index: token: prefix %q is ambiguous (%d matches)", prefix, len(hashes))
	}

	full := hashes[0]
	_, err = db.conn.Exec(`UPDATE tokens SET revoked = 1 WHERE token_hash = ?`, full)
	if err != nil {
		return "", fmt.Errorf("index: token: revoke update: %w", err)
	}
	return full, nil
}

// ListTokens returns all tokens for the given handle. If handle is empty,
// all tokens are returned.
func ListTokens(db *DB, handle string) ([]Token, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if handle == "" {
		rows, err = db.conn.Query(
			`SELECT token_hash, handle, created_at, expires_at, revoked, label
			 FROM tokens ORDER BY created_at DESC`,
		)
	} else {
		rows, err = db.conn.Query(
			`SELECT token_hash, handle, created_at, expires_at, revoked, label
			 FROM tokens WHERE handle = ? ORDER BY created_at DESC`,
			handle,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("index: token: list: %w", err)
	}
	defer rows.Close()

	var out []Token
	for rows.Next() {
		tok, err := scanToken(rows)
		if err != nil {
			return nil, fmt.Errorf("index: token: list scan: %w", err)
		}
		out = append(out, *tok)
	}
	return out, rows.Err()
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanToken(s scanner) (*Token, error) {
	var (
		tok        Token
		createdStr string
		expiresStr sql.NullString
		labelStr   sql.NullString
		revokedInt int
	)
	if err := s.Scan(&tok.Hash, &tok.Handle, &createdStr, &expiresStr, &revokedInt, &labelStr); err != nil {
		return nil, err
	}
	t, err := time.Parse(time.RFC3339, createdStr)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	tok.CreatedAt = t
	tok.Revoked = revokedInt != 0
	if labelStr.Valid {
		tok.Label = labelStr.String
	}
	if expiresStr.Valid && expiresStr.String != "" {
		et, err := time.Parse(time.RFC3339, expiresStr.String)
		if err != nil {
			return nil, fmt.Errorf("parse expires_at: %w", err)
		}
		tok.ExpiresAt = &et
	}
	return &tok, nil
}

// nullableString returns nil if s is empty, otherwise s (for SQL binding).
func nullableString(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
