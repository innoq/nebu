package db

// Story 7-35: ATDD — failing tests written BEFORE implementation (red phase).
// This file fails to compile until user_tx.go is created and withUserDB is defined.
// Once the function exists, these tests verify its contract.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

// ─── compile-time signature check ────────────────────────────────────────────

// TestWithUserDB_SignatureExists fails to compile until withUserDB is defined in user_tx.go.
// That compile failure is the ATDD red state for AC #1.
func TestWithUserDB_SignatureExists(t *testing.T) {
	var _ func(context.Context, *sql.DB, string, func(*sql.Tx) error) error = withUserDB
}

// ─── minimal mock driver (no external deps) ──────────────────────────────────

var mockUserTxConnCh = make(chan *userTxConn, 8)

func init() {
	sql.Register("usertx-mock", &userTxDriver{})
}

type userTxDriver struct{}

func (d *userTxDriver) Open(_ string) (driver.Conn, error) {
	conn := <-mockUserTxConnCh
	return conn, nil
}

type userTxConn struct {
	mu        sync.Mutex
	prepares  []string
	execArgs  [][]driver.Value
	activeTx  *userTxTx
	commitErr error
}

func (c *userTxConn) Prepare(query string) (driver.Stmt, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prepares = append(c.prepares, strings.TrimSpace(query))
	stmt := &userTxStmt{conn: c, idx: len(c.prepares) - 1}
	// Pre-allocate the matching args slot so the indices stay aligned.
	c.execArgs = append(c.execArgs, nil)
	return stmt, nil
}

func (c *userTxConn) Close() error { return nil }

func (c *userTxConn) Begin() (driver.Tx, error) {
	tx := &userTxTx{conn: c}
	c.mu.Lock()
	c.activeTx = tx
	c.mu.Unlock()
	return tx, nil
}

type userTxTx struct {
	conn       *userTxConn
	committed  bool
	rolledBack bool
}

func (t *userTxTx) Commit() error {
	t.committed = true
	return t.conn.commitErr
}

func (t *userTxTx) Rollback() error {
	t.rolledBack = true
	return nil
}

type userTxStmt struct {
	conn *userTxConn
	idx  int
}

func (s *userTxStmt) Close() error  { return nil }
func (s *userTxStmt) NumInput() int { return -1 }
func (s *userTxStmt) Exec(args []driver.Value) (driver.Result, error) {
	s.conn.mu.Lock()
	// args is owned by the caller; copy so we can read it after this call returns.
	captured := make([]driver.Value, len(args))
	copy(captured, args)
	s.conn.execArgs[s.idx] = captured
	s.conn.mu.Unlock()
	return driver.RowsAffected(0), nil
}
func (s *userTxStmt) Query(_ []driver.Value) (driver.Rows, error) { return &userTxRows{}, nil }

type userTxRows struct{}

func (r *userTxRows) Columns() []string           { return nil }
func (r *userTxRows) Close() error                { return nil }
func (r *userTxRows) Next(_ []driver.Value) error { return io.EOF }

// openMockDB puts a fresh recording conn into the channel, opens a DB, and
// forces a single connection so the conn we sent is the one that gets used.
func openMockDB(t *testing.T, commitErr error) (*sql.DB, *userTxConn) {
	t.Helper()
	conn := &userTxConn{commitErr: commitErr}
	mockUserTxConnCh <- conn
	db, err := sql.Open("usertx-mock", "test")
	if err != nil {
		t.Fatalf("sql.Open mock: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db, conn
}

// ─── behaviour tests ──────────────────────────────────────────────────────────

// TestWithUserDB_SetsGUCBeforeFn verifies that the first prepared statement inside
// the transaction sets app.user_id for RLS AND that the userID is passed as a
// bound parameter (not string-interpolated into the SQL) — this is the SQL-injection
// guarantee for the GUC value (AC #1).
//
// Accepts both SET LOCAL and set_config() forms. SET LOCAL x = $1 is invalid PostgreSQL
// syntax (SET commands reject bind parameters), so the implementation uses
// set_config('app.user_id', $1, true) which is equivalent but accepts prepared params.
func TestWithUserDB_SetsGUCBeforeFn(t *testing.T) {
	db, conn := openMockDB(t, nil)
	userID := "@kai:nebu.test"
	var fnCalled bool

	err := withUserDB(context.Background(), db, userID, func(tx *sql.Tx) error {
		fnCalled = true
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fnCalled {
		t.Fatal("fn was not called")
	}

	conn.mu.Lock()
	defer conn.mu.Unlock()

	if len(conn.prepares) == 0 {
		t.Fatal("no SQL statements were executed")
	}
	first := conn.prepares[0]
	lowerFirst := strings.ToLower(first)
	// Accept either SET LOCAL or set_config() — both scope the GUC to the transaction.
	setsGUC := strings.Contains(lowerFirst, "set local app.user_id") ||
		(strings.Contains(lowerFirst, "set_config") && strings.Contains(lowerFirst, "app.user_id"))
	if !setsGUC {
		t.Errorf("first statement must set app.user_id GUC (SET LOCAL or set_config); got: %q", first)
	}
	// SQL-injection guard: the SQL text must use a bind placeholder ($1), not the
	// literal userID string interpolated.
	if !strings.Contains(first, "$1") {
		t.Errorf("first statement must use $1 placeholder for userID; got: %q", first)
	}
	if strings.Contains(first, userID) {
		t.Errorf("userID must NOT be interpolated into SQL text; got: %q", first)
	}
	// The userID must be delivered as a bound driver.Value to the prepared statement.
	if len(conn.execArgs) == 0 || len(conn.execArgs[0]) == 0 {
		t.Fatal("GUC statement received no bound arguments")
	}
	gotArg := conn.execArgs[0][0]
	if gotArg != userID {
		t.Errorf("userID must be passed as bound parameter $1; got driver.Value %v (%T), want %q", gotArg, gotArg, userID)
	}
}

// TestWithUserDB_CommitsOnSuccess verifies that the transaction is committed when
// fn returns nil (AC #1).
func TestWithUserDB_CommitsOnSuccess(t *testing.T) {
	db, conn := openMockDB(t, nil)

	err := withUserDB(context.Background(), db, "@kai:nebu.test", func(tx *sql.Tx) error {
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	conn.mu.Lock()
	tx := conn.activeTx
	conn.mu.Unlock()

	if tx == nil {
		t.Fatal("no transaction was started")
	}
	if !tx.committed {
		t.Error("transaction was not committed on success")
	}
	if tx.rolledBack {
		t.Error("transaction must not be rolled back on success")
	}
}

// TestWithUserDB_RollbackOnFnError verifies that when fn returns an error,
// withUserDB returns that error and the transaction is rolled back (AC #1).
func TestWithUserDB_RollbackOnFnError(t *testing.T) {
	db, conn := openMockDB(t, nil)
	sentinel := errors.New("fn error")

	err := withUserDB(context.Background(), db, "@kai:nebu.test", func(tx *sql.Tx) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}

	conn.mu.Lock()
	tx := conn.activeTx
	conn.mu.Unlock()

	if tx == nil {
		t.Fatal("no transaction was started")
	}
	if tx.committed {
		t.Error("transaction must not be committed when fn returns error")
	}
	if !tx.rolledBack {
		t.Error("transaction must be rolled back when fn returns error")
	}
}

// TestWithUserDB_RollbackOnCommitError verifies that a commit failure is returned
// and the deferred rollback runs (AC #1: "rolls back on any error from fn or from commit").
func TestWithUserDB_RollbackOnCommitError(t *testing.T) {
	commitErr := errors.New("commit failed")
	db, conn := openMockDB(t, commitErr)

	err := withUserDB(context.Background(), db, "@kai:nebu.test", func(tx *sql.Tx) error {
		return nil
	})
	if !errors.Is(err, commitErr) {
		t.Fatalf("expected commit error, got %v", err)
	}

	conn.mu.Lock()
	tx := conn.activeTx
	conn.mu.Unlock()

	if tx == nil {
		t.Fatal("no transaction was started")
	}
	if !tx.committed {
		t.Error("Commit must have been called even though it errored")
	}
}
