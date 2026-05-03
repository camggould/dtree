package migrations

import "database/sql"

func init() {
	Register(Migration{
		From: 1,
		To:   2,
		Name: "v1_to_v2_tokens_table",
		Apply: func(tx *sql.Tx) error {
			stmts := []string{
				`CREATE TABLE IF NOT EXISTS tokens (
					token_hash TEXT PRIMARY KEY,
					handle     TEXT NOT NULL,
					created_at TEXT NOT NULL,
					expires_at TEXT,
					revoked    INTEGER NOT NULL DEFAULT 0,
					label      TEXT
				)`,
				`CREATE INDEX IF NOT EXISTS idx_tokens_handle ON tokens(handle)`,
			}
			for _, s := range stmts {
				if _, err := tx.Exec(s); err != nil {
					return err
				}
			}
			return nil
		},
	})
}
