// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// closeFailingRows — driver.Rows wrapper whose Close() returns an injected
// error after closing the real underlying rows (to avoid resource leaks).
// =============================================================================

type closeFailingRows struct {
	driver.Rows
	closeErr error
}

func (r *closeFailingRows) Close() error {
	// Close the real rows first — prevents resource leaks in the test process.
	_ = r.Rows.Close()
	return r.closeErr
}

// =============================================================================
// closeFailingConnector — driver.Connector that injects a Close error.
// Connect() opens a real sqlite connection and wraps it in closeFailingConn,
// which returns *closeFailingRows from QueryContext.
// =============================================================================

type closeFailingConnector struct {
	dbPath   string
	closeErr error
}

func (c *closeFailingConnector) Connect(ctx context.Context) (driver.Conn, error) {
	realConn, err := sqliteDriver.Open(c.dbPath)
	if err != nil {
		return nil, err
	}
	return &closeFailingConn{
		conn:     realConn,
		closeErr: c.closeErr,
	}, nil
}

func (c *closeFailingConnector) Driver() driver.Driver {
	return sqliteDriver
}

// closeFailingConn wraps a real driver.Conn and returns *closeFailingRows from
// QueryContext. All other methods delegate to the real connection.
type closeFailingConn struct {
	conn     driver.Conn
	closeErr error
}

func (c *closeFailingConn) Prepare(query string) (driver.Stmt, error) {
	return c.conn.Prepare(query)
}

func (c *closeFailingConn) Close() error { return c.conn.Close() }

func (c *closeFailingConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *closeFailingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if bc, ok := c.conn.(driver.ConnBeginTx); ok {
		return bc.BeginTx(ctx, opts)
	}
	// Fallback for drivers that don't implement ConnBeginTx.
	return c.conn.Begin() //nolint:staticcheck // SA1019: fallback for older drivers
}

func (c *closeFailingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if qc, ok := c.conn.(driver.QueryerContext); ok {
		rows, err := qc.QueryContext(ctx, query, args)
		if err != nil {
			return nil, err
		}
		return &closeFailingRows{Rows: rows, closeErr: c.closeErr}, nil
	}
	return nil, driver.ErrSkip
}

func (c *closeFailingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if ec, ok := c.conn.(driver.ExecerContext); ok {
		return ec.ExecContext(ctx, query, args)
	}
	return nil, driver.ErrSkip
}

// =============================================================================
// errFailingRows — driver.Rows wrapper that injects an iteration error.
//
// Next() delegates to the real rows, and when the real rows are exhausted
// (real Next returns io.EOF), returns the injected iterErr instead. This causes
// database/sql to set lasterr, which then surfaces via (*sql.Rows).Err().
//
// Err() is also provided for driver-portability: if database/sql ever adds an
// optional Err() interface check on driver.Rows, this will be called.
// =============================================================================

type errFailingRows struct {
	driver.Rows
	iterErr error
}

// Next delegates to the real rows. When real iteration completes (io.EOF),
// the injected iterErr is returned instead, causing (*sql.Rows).Err() to
// return non-nil and triggering the defensive rows.Err() branch.
func (r *errFailingRows) Next(dest []driver.Value) error {
	err := r.Rows.Next(dest)
	if err == io.EOF {
		return r.iterErr
	}
	return err
}

// Err returns the injected iteration error. This exists for driver-portability:
// if a future version of database/sql checks for an optional Err() method on
// driver.Rows, this will be called.
func (r *errFailingRows) Err() error {
	return r.iterErr
}

// =============================================================================
// errFailingConnector — driver.Connector that injects an iteration error.
// Connect() opens a real sqlite connection and wraps it in errFailingConn,
// which returns *errFailingRows from QueryContext.
// =============================================================================

type errFailingConnector struct {
	dbPath  string
	iterErr error
}

func (c *errFailingConnector) Connect(ctx context.Context) (driver.Conn, error) {
	realConn, err := sqliteDriver.Open(c.dbPath)
	if err != nil {
		return nil, err
	}
	return &errFailingConn{
		conn:    realConn,
		iterErr: c.iterErr,
	}, nil
}

func (c *errFailingConnector) Driver() driver.Driver {
	return sqliteDriver
}

// errFailingConn wraps a real driver.Conn and returns *errFailingRows from
// QueryContext. All other methods delegate to the real connection.
type errFailingConn struct {
	conn    driver.Conn
	iterErr error
}

func (c *errFailingConn) Prepare(query string) (driver.Stmt, error) {
	return c.conn.Prepare(query)
}

func (c *errFailingConn) Close() error { return c.conn.Close() }

func (c *errFailingConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *errFailingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if bc, ok := c.conn.(driver.ConnBeginTx); ok {
		return bc.BeginTx(ctx, opts)
	}
	return c.conn.Begin() //nolint:staticcheck // SA1019: fallback for older drivers
}

func (c *errFailingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if qc, ok := c.conn.(driver.QueryerContext); ok {
		rows, err := qc.QueryContext(ctx, query, args)
		if err != nil {
			return nil, err
		}
		return &errFailingRows{Rows: rows, iterErr: c.iterErr}, nil
	}
	return nil, driver.ErrSkip
}

func (c *errFailingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if ec, ok := c.conn.(driver.ExecerContext); ok {
		return ec.ExecContext(ctx, query, args)
	}
	return nil, driver.ErrSkip
}

// =============================================================================
// helper: openDBWithConnector creates a temp-dir SQLite database, creates the
// tasks and settings tables, and returns the *sql.DB opened with the given
// connector. The caller must Close the DB.
// =============================================================================

func openDBWithConnector(t *testing.T, connector driver.Connector) *sql.DB {
	t.Helper()

	db := sql.OpenDB(connector)
	t.Cleanup(func() { _ = db.Close() })

	// Create standard tables.
	if _, err := db.Exec(`CREATE TABLE tasks (
		id INTEGER PRIMARY KEY,
		content TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		created_at TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("failed to create tasks table: %v", err)
	}

	if _, err := db.Exec(`CREATE TABLE settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("failed to create settings table: %v", err)
	}

	return db
}

// =============================================================================
// TestSQLiteTaskStore_QueryOrdered_CloseError — covers gaps #5-#6:
// the rows.Close() defer error-shadowing in queryOrdered (L140-145).
//
// Mechanism: closeFailingRows.Close() returns an injected error. When Next()
// returns io.EOF, database/sql calls driver.Rows.Close() internally, storing
// the error in lasterr.  rows.Err() then returns it (L158-160), wrapping as
// "tasks rows iteration error".  The defer on L140-145 cannot be triggered
// directly — by the time it runs, rs.closed is true so rows.Close() returns
// nil.  This test still validates that a driver Close error is properly
// surfaced (via the Err() path) rather than silently swallowed.
// =============================================================================

func TestSQLiteTaskStore_QueryOrdered_CloseError(t *testing.T) {
	t.Parallel()

	connector := &closeFailingConnector{
		dbPath:   filepath.Join(t.TempDir(), "close_error.db"),
		closeErr: errors.New("close failed"),
	}

	db := openDBWithConnector(t, connector)
	store := newSQLiteTaskStore(db)

	// Insert a valid row — query and scan must succeed so the Close error
	// becomes the sole return value via the defer error-shadowing.
	now := time.Now()
	_, err := db.Exec("INSERT INTO tasks (id, content, status, created_at) VALUES (?, ?, ?, ?)",
		1, "test task", "pending", now.Format(time.RFC3339Nano))
	require.NoError(t, err)

	result, err := store.Query(context.Background(), ports.ListFilter{}, 0, 0)
	require.Error(t, err)
	// The close error surfaces through rows.Err() (L158-160), not the defer
	// (L140-145), because database/sql calls driver.Rows.Close() internally
	// when Next() returns io.EOF.  The close error is stored in lasterr and
	// surfaced via rows.Err() → "tasks rows iteration error: close failed".
	assert.Contains(t, err.Error(), "tasks rows iteration error")
	assert.Contains(t, err.Error(), "close failed")
	// On error, queryOrdered returns (nil, wrappedErr).
	assert.Nil(t, result)
}

// =============================================================================
// TestSQLiteTaskStore_QueryOrdered_RowsErr — covers gap #7:
// the rows.Err() check in queryOrdered (L158-160).
//
// When rows iteration completes but the driver surfaces an iteration error
// via Next() returning a non-EOF error, rows.Err() returns non-nil and the
// production code wraps it as "tasks rows iteration error".
// =============================================================================

func TestSQLiteTaskStore_QueryOrdered_RowsErr(t *testing.T) {
	t.Parallel()

	connector := &errFailingConnector{
		dbPath:  filepath.Join(t.TempDir(), "rows_err.db"),
		iterErr: errors.New("iteration failed"),
	}

	db := openDBWithConnector(t, connector)
	store := newSQLiteTaskStore(db)

	now := time.Now()
	_, err := db.Exec("INSERT INTO tasks (id, content, status, created_at) VALUES (?, ?, ?, ?)",
		1, "test task", "pending", now.Format(time.RFC3339Nano))
	require.NoError(t, err)

	_, err = store.Query(context.Background(), ports.ListFilter{}, 0, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tasks rows iteration error")
	assert.Contains(t, err.Error(), "iteration failed")
}

// =============================================================================
// TestSQLiteKVStore_GetAll_CloseError — covers gaps #8-#9:
// the rows.Close() defer error-shadowing in GetAll (L251-256).
//
// Mechanism: Same as the task-store CloseError test — the Close error is
// captured by database/sql's internal close (triggered when Next returns
// io.EOF), stored in lasterr, and surfaced via rows.Err() (L263-265), not
// via the defer.  The error is wrapped as "iterating settings rows".
// The returned map must be nil (GetAll returns nil, wrappedErr on error).
// =============================================================================

func TestSQLiteKVStore_GetAll_CloseError(t *testing.T) {
	t.Parallel()

	connector := &closeFailingConnector{
		dbPath:   filepath.Join(t.TempDir(), "kv_close_error.db"),
		closeErr: errors.New("close failed"),
	}

	db := openDBWithConnector(t, connector)
	store := newSQLiteKVStore(db)

	_, err := db.Exec("INSERT INTO settings (key, value) VALUES (?, ?)", "theme", "dark")
	require.NoError(t, err)

	result, err := store.GetAll(context.Background())
	require.Error(t, err)
	// The close error surfaces via rows.Err() → "iterating settings rows: close failed".
	assert.Contains(t, err.Error(), "iterating settings rows")
	assert.Contains(t, err.Error(), "close failed")
	assert.Nil(t, result)
}

// =============================================================================
// TestSQLiteKVStore_GetAll_RowsErr — covers gap #10:
// the rows.Err() check in GetAll (L263-265).
//
// When settings rows iteration completes but the driver surfaces an iteration
// error, rows.Err() returns non-nil and the production code wraps it as
// "iterating settings rows".
// =============================================================================

func TestSQLiteKVStore_GetAll_RowsErr(t *testing.T) {
	t.Parallel()

	connector := &errFailingConnector{
		dbPath:  filepath.Join(t.TempDir(), "kv_rows_err.db"),
		iterErr: errors.New("iteration failed"),
	}

	db := openDBWithConnector(t, connector)
	store := newSQLiteKVStore(db)

	_, err := db.Exec("INSERT INTO settings (key, value) VALUES (?, ?)", "theme", "dark")
	require.NoError(t, err)

	_, err = store.GetAll(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "iterating settings rows")
	assert.Contains(t, err.Error(), "iteration failed")
}
