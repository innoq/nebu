package db

import (
	"context"
	"database/sql"
)

// withUserDB runs fn inside a PostgreSQL transaction with set_config('app.user_id', userID, true)
// called at the start. The third argument (is_local=true) is equivalent to SET LOCAL, so the
// GUC is automatically reset when the transaction ends — no leakage to pooled connections.
// This satisfies RLS policies using current_setting('app.user_id', true) — see migrations 000029 and 000033.
//
// NOTE: SET LOCAL x = $1 is invalid PostgreSQL syntax (SET commands reject bind parameters).
// set_config() is a regular function call and works correctly with prepared statements.
func withUserDB(ctx context.Context, db *sql.DB, userID string, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck — safe after Commit (returns ErrTxDone)

	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.user_id', $1, true)`, userID); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
