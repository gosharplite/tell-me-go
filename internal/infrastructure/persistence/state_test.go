// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func verifyStateInitialization(t *testing.T, state ports.SessionProvider) {
	t.Helper()
	if state.GetTasks() == nil {
		t.Error("expected all services to be initialized")
	}
}

func TestNewSessionState_FileStorage(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	ctx := context.Background()

	state, err := NewSessionState(ctx, tempDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = state.Close() }()

	verifyStateInitialization(t, state)

	info := state.GetInfo()
	if info.Env["STORAGE_TYPE"] != "sqlite" {
		t.Errorf("expected STORAGE_TYPE to be sqlite, got %s", info.Env["STORAGE_TYPE"])
	}

	if info.Paths["config_dir"] != tempDir {
		t.Errorf("expected config_dir to be %s, got %s", tempDir, info.Paths["config_dir"])
	}
}

func TestNewSessionState_MemoryStorage(t *testing.T) {
	tempDir := t.TempDir()
	ctx := context.Background()

	t.Setenv("STORAGE_TYPE", "memory")
	state, err := NewSessionState(ctx, tempDir)
	if err != nil {
		t.Fatal(err)
	}

	verifyStateInitialization(t, state)

	info := state.GetInfo()
	if info.Env["STORAGE_TYPE"] != "memory" {
		t.Errorf("expected STORAGE_TYPE to be memory, got %s", info.Env["STORAGE_TYPE"])
	}
}

func TestSessionState_Persistence(t *testing.T) {
	tempDir := t.TempDir()
	ctx := context.Background()

	// 1. Create a session and set info
	state1, err := NewSessionState(ctx, tempDir)
	if err != nil {
		t.Fatal(err)
	}

	info := state1.GetInfo()
	info.ActiveToolkits = []string{"git", "k8s"}
	state1.SetInfo(info)
	_ = state1.Close()

	// 2. Reload the session state from disk
	state2, err := NewSessionState(ctx, tempDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = state2.Close() }()

	// 3. Verify ActiveToolkits is restored
	restoredInfo := state2.GetInfo()
	if len(restoredInfo.ActiveToolkits) != 2 {
		t.Fatalf("expected 2 active toolkits, got %d", len(restoredInfo.ActiveToolkits))
	}

	tkMap := make(map[string]bool)
	for _, tk := range restoredInfo.ActiveToolkits {
		tkMap[tk] = true
	}

	if !tkMap["git"] || !tkMap["k8s"] {
		t.Errorf("restored toolkits missing git or k8s: %v", restoredInfo.ActiveToolkits)
	}
}

func TestSessionState_GetSettings(t *testing.T) {
	tempDir := t.TempDir()
	ctx := context.Background()

	t.Setenv("STORAGE_TYPE", "memory")
	state, err := NewSessionState(ctx, tempDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = state.Close() }()

	if state.GetSettings() == nil {
		t.Error("expected GetSettings() to return a non-nil KVStore")
	}
}

func TestSessionState_GetHealthChecker_SQLite(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	ctx := context.Background()

	// Default STORAGE_TYPE is "sqlite" — creates a real DB.
	state, err := NewSessionState(ctx, tempDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = state.Close() }()

	checker := state.GetHealthChecker()

	// Verify it's the real health checker, not the no-op fallback.
	if checker == nil {
		t.Fatal("expected non-nil health checker")
	}
	if _, ok := checker.(*sqliteHealthChecker); !ok {
		t.Errorf("expected *sqliteHealthChecker, got %T", checker)
	}
}

func TestSessionState_GetHealthChecker_Memory(t *testing.T) {
	tempDir := t.TempDir()
	ctx := context.Background()

	t.Setenv("STORAGE_TYPE", "memory")
	state, err := NewSessionState(ctx, tempDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = state.Close() }()

	checker := state.GetHealthChecker()
	if checker == nil {
		t.Fatal("expected non-nil health checker")
	}
	if _, ok := checker.(*noOpHealthChecker); !ok {
		t.Errorf("expected *noOpHealthChecker, got %T", checker)
	}
}

func TestNewSessionState_InitRepositoriesFailure(t *testing.T) {
	t.Parallel()

	// Use a path where the parent directory does not exist.
	// initSQLiteDB will succeed at sql.Open but ExecContext will fail
	// because the driver cannot create the DB file in a nonexistent directory.
	badDir := filepath.Join(t.TempDir(), "nonexistent", "subdir")
	ctx := context.Background()

	_, err := NewSessionState(ctx, badDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "initializing persistence repositories",
		"error should be wrapped with initialization context")
}

func TestNewSessionState_InitServicesSucceeds(t *testing.T) {
	// NOT parallel: overrides package-level sqlOpenFn, must run sequentially.
	//
	// After Issue #906, initServices is a no-op that never queries the DB.
	// Even with a connector that would fail on a 6th operation, NewSessionState
	// succeeds because initServices no longer performs any DB operations.

	ctx := context.Background()
	tempDir := t.TempDir()

	var opCount atomic.Int32
	origOpen := sqlOpenFn
	t.Cleanup(func() { sqlOpenFn = origOpen })

	sqlOpenFn = func(driverName, dsn string) (*sql.DB, error) {
		return sql.OpenDB(&initServicesFailingConnector{
			dsn:       dsn,
			opCount:   &opCount,
			failAfter: 5, // initSQLiteDB does ~5 ops; initServices does none
		}), nil
	}

	state, err := NewSessionState(ctx, tempDir)
	require.NoError(t, err, "NewSessionState should succeed since initServices is a no-op")
	require.NotNil(t, state)
	_ = state.Close()
}

// failingTaskStore is a ListStore[ports.Task] whose ReadAll always fails.
type failingTaskStore struct{}

func (s *failingTaskStore) ReadAll(ctx context.Context) ([]ports.Task, error) {
	return nil, errors.New("simulated read failure")
}
func (s *failingTaskStore) Update(ctx context.Context, id int64, item ports.Task) error { return nil }
func (s *failingTaskStore) Delete(ctx context.Context, id int64) error                  { return nil }
func (s *failingTaskStore) DeleteAll(ctx context.Context) error                         { return nil }
func (s *failingTaskStore) Append(ctx context.Context, item ports.Task) error           { return nil }
func (s *failingTaskStore) Query(ctx context.Context, filter ports.ListFilter, limit, offset int) ([]ports.Task, error) {
	return nil, errors.New("simulated query failure")
}
func (s *failingTaskStore) Count(ctx context.Context) (int, error) {
	return 0, errors.New("simulated count failure")
}

func TestSessionState_HydrateInfo_CorruptedStateFile(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	ctx := context.Background()

	// Write corrupted JSON into state.json — this exercises the
	// json.Unmarshal error branch inside hydrateInfo.
	corrupted := []byte("{invalid json}}}}")
	if err := os.WriteFile(filepath.Join(tempDir, "state.json"), corrupted, 0644); err != nil {
		t.Fatal(err)
	}

	state, err := NewSessionState(ctx, tempDir)
	if err != nil {
		t.Fatalf("NewSessionState should succeed with corrupted state file, got: %v", err)
	}
	defer func() { _ = state.Close() }()

	info := state.GetInfo()

	t.Run("env_defaults", func(t *testing.T) {
		runHydrateInfoCheckEnv(t, info)
	})
	t.Run("paths_defaults", func(t *testing.T) {
		runHydrateInfoCheckPaths(t, info, tempDir)
	})
	t.Run("active_toolkits_default", func(t *testing.T) {
		runHydrateInfoCheckToolkits(t, info)
	})
	t.Run("model_provider_defaults", func(t *testing.T) {
		runHydrateInfoCheckModelProvider(t, info)
	})
}

func runHydrateInfoCheckEnv(t *testing.T, info ports.SessionInfo) {
	t.Helper()
	// Corrupted unmarshal is silently swallowed; defaults must be populated.
	if info.Env == nil {
		t.Error("expected non-nil Env map (default fallback)")
	}
	if info.Env["STORAGE_TYPE"] != "sqlite" {
		t.Errorf("expected STORAGE_TYPE to be sqlite, got %s", info.Env["STORAGE_TYPE"])
	}
}

func runHydrateInfoCheckPaths(t *testing.T, info ports.SessionInfo, tempDir string) {
	t.Helper()
	if info.Paths == nil {
		t.Error("expected non-nil Paths map (default fallback)")
	}
	if info.Paths["config_dir"] != tempDir {
		t.Errorf("expected config_dir to be %s, got %s", tempDir, info.Paths["config_dir"])
	}
}

func runHydrateInfoCheckToolkits(t *testing.T, info ports.SessionInfo) {
	t.Helper()
	if info.ActiveToolkits == nil {
		t.Error("expected non-nil ActiveToolkits slice (default fallback)")
	}
	if len(info.ActiveToolkits) != 0 {
		t.Errorf("expected empty ActiveToolkits, got %v", info.ActiveToolkits)
	}
}

func runHydrateInfoCheckModelProvider(t *testing.T, info ports.SessionInfo) {
	t.Helper()
	if info.Model != "" {
		t.Errorf("expected Model to be empty, got %q", info.Model)
	}
	if info.Provider != "" {
		t.Errorf("expected Provider to be empty, got %q", info.Provider)
	}
}

func TestSessionState_SetInfo_PersistError(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("os.Chmod does not prevent file creation on Windows")
	}

	if os.Geteuid() == 0 {
		t.Skip("skipping permission test: running as root")
	}

	ctx := context.Background()
	tempDir := t.TempDir()

	state, err := NewSessionState(ctx, tempDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = state.Close() }()

	// Create a read-only directory to make AtomicWrite fail.
	readOnlyDir := filepath.Join(t.TempDir(), "readonly")
	if err := os.Mkdir(readOnlyDir, 0755); err != nil {
		t.Fatalf("failed to create read-only dir: %v", err)
	}
	if err := os.Chmod(readOnlyDir, 0555); err != nil {
		t.Fatalf("failed to chmod dir: %v", err)
	}
	defer func() { _ = os.Chmod(readOnlyDir, 0755) }()

	// Point statePath inside the read-only directory so persistence fails.
	ss := state.(*sessionState)
	ss.statePath = filepath.Join(readOnlyDir, "state.json")

	// Inject a logger that writes to a buffer so we can assert the
	// persistence error is logged.
	var buf bytes.Buffer
	ss.logger = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Build updated info — copy from current so Env/Paths are intact.
	info := ss.GetInfo()
	info.ActiveToolkits = []string{"git", "k8s", "ado"}

	// SetInfo must not panic even when persistence fails.
	state.SetInfo(info)

	// Verify the persistence error was logged.
	logOutput := buf.String()
	assert.Contains(t, logOutput, "failed to persist session state",
		"persistence error must be logged so operators can diagnose disk failures")
	assert.Contains(t, logOutput, ss.statePath,
		"log message must include the path that failed")

	// In-memory state is updated regardless of persistence outcome.
	restored := state.GetInfo()
	if len(restored.ActiveToolkits) != 3 {
		t.Errorf("expected 3 active toolkits in memory, got %d", len(restored.ActiveToolkits))
	}
}

func TestSessionState_SetInfo_IsolationFromCallerMutation(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	// Use memory storage so no disk I/O interferes
	t.Setenv("STORAGE_TYPE", "memory")
	state, err := NewSessionState(ctx, tempDir)
	require.NoError(t, err)
	defer func() { _ = state.Close() }()

	// Build info with known values
	info := state.GetInfo()
	info.Env["caller_key"] = "original"
	info.Paths["caller_path"] = "/original"
	info.ActiveToolkits = []string{"git"}

	// Store it
	state.SetInfo(info)

	// Mutate the caller's references AFTER SetInfo
	info.Env["caller_key"] = "mutated"
	info.Paths["caller_path"] = "/mutated"
	info.ActiveToolkits[0] = "corrupted"

	// Read back — stored state must be ISOLATED from caller mutation
	stored := state.GetInfo()

	assert.Equal(t, "original", stored.Env["caller_key"],
		"Env map must be a deep copy; caller mutation must not affect stored state")
	assert.Equal(t, "/original", stored.Paths["caller_path"],
		"Paths map must be a deep copy; caller mutation must not affect stored state")
	assert.Equal(t, []string{"git"}, stored.ActiveToolkits,
		"ActiveToolkits slice must be a deep copy; caller mutation must not affect stored state")
}

func TestInitServices_InitializeIsNoOp(t *testing.T) {
	t.Parallel()

	// After Issue #906, Initialize is a no-op that always returns nil.
	// initServices ignores the return value. Even with a failing task store,
	// initServices succeeds because it never calls any store methods during init.
	ctx := context.Background()
	tasks, err := initServices(ctx, &failingTaskStore{})
	require.NoError(t, err, "initServices should succeed since Initialize is a no-op")
	require.NotNil(t, tasks)
}

func TestInitRepositories_MigrationFailure(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("file permission test not reliable on Windows")
	}

	ctx := context.Background()
	tempDir := t.TempDir()

	// Create tasks.json with mode 0000 so ReadFile fails with EACCES.
	tasksPath := filepath.Join(tempDir, "tasks.json")
	if err := os.WriteFile(tasksPath, []byte(`[{"id":1,"content":"T","status":"pending","created_at":"2025-01-01T00:00:00Z"}]`), 0000); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(tasksPath, 0644) }()

	_, _, db, _, err := initRepositories(ctx, tempDir, "sqlite")

	// db must be nil (closed) on error, not leaked.
	assert.Nil(t, db, "db should be nil after initRepositories error")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "migrating legacy tasks",
		"error should contain migration failure context")
}

// initServicesFailingConnector wraps the SQLite driver and returns connections
// whose QueryContext and ExecContext fail after a deterministic number of
// successful operations. This allows initRepositories to complete normally
// (5 ops: 2 pragmas + 2 CREATE TABLE + 1 COUNT) while initServices is
// verified to not perform any DB operations (it's a no-op after Issue #906).
type initServicesFailingConnector struct {
	dsn       string
	opCount   *atomic.Int32
	failAfter int32
}

func (c *initServicesFailingConnector) Connect(ctx context.Context) (driver.Conn, error) {
	realConn, err := sqliteDriver.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return &initServicesFailingConn{
		conn:      realConn,
		opCount:   c.opCount,
		failAfter: c.failAfter,
	}, nil
}

func (c *initServicesFailingConnector) Driver() driver.Driver { return sqliteDriver }

type initServicesFailingConn struct {
	conn      driver.Conn
	opCount   *atomic.Int32
	failAfter int32
}

func (c *initServicesFailingConn) Prepare(q string) (driver.Stmt, error) { return c.conn.Prepare(q) }
func (c *initServicesFailingConn) Close() error                          { return c.conn.Close() }
func (c *initServicesFailingConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *initServicesFailingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if bc, ok := c.conn.(driver.ConnBeginTx); ok {
		return bc.BeginTx(ctx, opts)
	}
	// Fallback for drivers that don't implement ConnBeginTx.
	return c.conn.Begin() //nolint:staticcheck // SA1019: fallback for older drivers
}

func (c *initServicesFailingConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	n := c.opCount.Add(1)
	if n > c.failAfter {
		return nil, errors.New("injected initServices failure")
	}
	if qc, ok := c.conn.(driver.QueryerContext); ok {
		return qc.QueryContext(ctx, q, args)
	}
	return nil, driver.ErrSkip
}

func (c *initServicesFailingConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	n := c.opCount.Add(1)
	if n > c.failAfter {
		return nil, errors.New("injected initServices failure")
	}
	if ec, ok := c.conn.(driver.ExecerContext); ok {
		return ec.ExecContext(ctx, q, args)
	}
	return nil, driver.ErrSkip
}

func TestNewSessionState_InitServicesError(t *testing.T) {
	// NOT parallel: overrides package-level initServicesFn, must run sequentially.

	ctx := context.Background()
	tempDir := t.TempDir()

	origInit := initServicesFn
	t.Cleanup(func() { initServicesFn = origInit })

	injectedErr := errors.New("injected initServices failure")
	initServicesFn = func(ctx context.Context, _ ports.ListStore[ports.Task]) (ports.TaskStore, error) {
		return nil, injectedErr
	}

	state, err := NewSessionState(ctx, tempDir)
	assert.Nil(t, state, "state should be nil when initServices fails")
	require.Error(t, err)
	assert.ErrorIs(t, err, injectedErr, "error should be the injected initServices error")
}

func TestSessionState_Close_WALCheckpointError(t *testing.T) {
	// NOT parallel: overrides slog.Default(), must run sequentially.
	var buf bytes.Buffer
	origLogger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(origLogger) })
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))

	ctx := context.Background()
	tempDir := t.TempDir()

	state, err := NewSessionState(ctx, tempDir)
	require.NoError(t, err)

	ss := state.(*sessionState)
	require.NotNil(t, ss.db, "expected non-nil db in sessionState")

	require.NoError(t, ss.db.Close(), "pre-closing db for test setup")

	_ = state.Close()
	// sql.DB.Close() is idempotent and returns nil on an already-closed DB,
	// so we do not assert on the error here. The key behavior under test is
	// the WAL checkpoint failure being logged.

	assert.Contains(t, buf.String(), "WAL checkpoint on close failed")
}
