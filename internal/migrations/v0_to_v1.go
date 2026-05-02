package migrations

import "database/sql"

func init() {
	Register(Migration{
		From: 0,
		To:   1,
		Name: "v0_to_v1_baseline",
		Apply: func(tx *sql.Tx) error {
			// Insert schema_version=1 if not already present. Fresh DBs created by
			// index.Open already have this row (CreateSchema stamps it), so the
			// ON CONFLICT DO NOTHING makes this step idempotent.
			_, err := tx.Exec(
				`INSERT INTO _meta(key, value) VALUES('schema_version', 1)
				 ON CONFLICT(key) DO NOTHING`,
			)
			return err
		},
	})
}
