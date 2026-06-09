// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
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
	if err == nil {
		t.Error("expected error from NewSessionState with invalid path, got nil")
	}
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

	// Build updated info — copy from current so Env/Paths are intact.
	info := ss.GetInfo()
	info.ActiveToolkits = []string{"git", "k8s", "ado"}

	// SetInfo must not panic even when persistence fails.
	state.SetInfo(info)

	// In-memory state is updated regardless of persistence outcome.
	restored := state.GetInfo()
	if len(restored.ActiveToolkits) != 3 {
		t.Errorf("expected 3 active toolkits in memory, got %d", len(restored.ActiveToolkits))
	}
}

func TestInitServices_InitializeFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, err := initServices(ctx, &failingTaskStore{})
	if err == nil {
		t.Fatal("expected error from initServices when Initialize fails, got nil")
	}
	if !strings.Contains(err.Error(), "simulated query failure") {
		t.Errorf("expected error to contain 'simulated query failure', got: %v", err)
	}
}
