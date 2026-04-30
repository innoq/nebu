package db

import (
	"context"
	"database/sql"
)

// withUserDB runs fn inside a PostgreSQL transaction that has SET LOCAL app.user_id = $1
// set for the duration of the transaction. This is required for Row-Level Security policies
// that use current_setting('app.user_id', true) — see migrations 000029 and 000033.
//
// SET LOCAL is used (not SET) so the GUC is automatically reset when the transaction ends,
// preventing GUC leakage to subsequent connections returned to the pool.
func withUserDB(ctx context.Context, db *sql.DB, userID string, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck — safe after Commit (returns ErrTxDone)

	if _, err := tx.ExecContext(ctx, `SET LOCAL app.user_id = $1`, userID); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
