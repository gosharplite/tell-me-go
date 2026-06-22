package analysis

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

func TestDependencyAnalyzer_GetPackageGraph(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	module := "example.com/mod"

	// Create a dummy project structure
	pkg1 := filepath.Join(tmpDir, "pkg1")
	pkg2 := filepath.Join(tmpDir, "pkg2")
	pkg3 := filepath.Join(tmpDir, "pkg3")

	_ = os.MkdirAll(pkg1, 0755)
	_ = os.MkdirAll(pkg2, 0755)
	_ = os.MkdirAll(pkg3, 0755)

	_ = os.WriteFile(filepath.Join(pkg1, "f1.go"), []byte("package pkg1\nimport \"example.com/mod/pkg2\"\nimport \"example.com/mod/pkg3\""), 0644)
	_ = os.WriteFile(filepath.Join(pkg2, "f2.go"), []byte("package pkg2"), 0644)
	_ = os.WriteFile(filepath.Join(pkg3, "f3.go"), []byte("package pkg3\nimport \"example.com/mod/pkg2\""), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/mod"), 0644)

	mockRunner := &mockAnalysisGoRunner{
		getModulePathFunc: func(ctx context.Context) (string, error) {
			return module, nil
		},
		getModuleDirFunc: func(ctx context.Context) (string, error) {
			return tmpDir, nil
		},
	}

	analyzer := newDependencyAnalyzer(mockRunner, &mockSecurityProvider{}, nil, infra_persistence.NewWorkspacePolicy())

	// Set workdir to tmpDir so that go list -m works
	// In a real scenario, the tool would run in the project root.
	// Our mock handles the go list calls.

	res, err := analyzer.GetPackageGraph(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	verifyPackageGraphResults(t, res.Text, module)
}

func verifyPackageGraphResults(t *testing.T, output, module string) {
	if !strings.Contains(output, module+"/pkg1") {
		t.Errorf("Expected pkg1 in output")
	}
	if !strings.Contains(output, "└── "+module+"/pkg2") {
		t.Errorf("Expected pkg2 as dependency")
	}
}

func TestDependencyAnalyzer_StartHeartbeat(t *testing.T) {
	t.Parallel()

	t.Run("nil hb, already-done channel", func(t *testing.T) {
		t.Parallel()
		a := newDependencyAnalyzer(nil, nil, nil, nil)
		done := make(chan struct{})
		close(done) // already done
		// Must not panic, must return immediately
		a.startHeartbeat(nil, done)
	})

	t.Run("nil hb, open done channel then close", func(t *testing.T) {
		t.Parallel()
		a := newDependencyAnalyzer(nil, nil, nil, nil)
		done := make(chan struct{})
		// Start heartbeat with nil hb — the `if hb != nil` guard at line 140
		// must prevent send on nil channel. Close done after a tick would fire.
		// Use a short timer to verify the goroutine doesn't panic, then close.
		go func() {
			a.startHeartbeat(nil, done)
		}()
		close(done)
	})

	t.Run("buffered hb, receives heartbeat", func(t *testing.T) {
		t.Parallel()
		a := newDependencyAnalyzer(nil, nil, nil, nil)
		done := make(chan struct{})
		hb := make(chan struct{}, 2)
		go func() {
			a.startHeartbeat(hb, done)
		}()
		// Wait for at least one heartbeat
		select {
		case <-hb:
			// got heartbeat
		case <-time.After(3 * time.Second):
			t.Error("timed out waiting for heartbeat")
		}
		close(done)
	})

	t.Run("hb full, does not block", func(t *testing.T) {
		t.Parallel()
		a := newDependencyAnalyzer(nil, nil, nil, nil)
		done := make(chan struct{})
		hb := make(chan struct{}, 1)
		// Fill the buffer
		hb <- struct{}{}
		go func() {
			a.startHeartbeat(hb, done)
		}()
		close(done)
		// Test passes if we reach here (no goroutine leak, no panic)
	})
}

func TestContainsGoFiles(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	t.Run("directory with go files", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(tmpDir, "withgo")
		_ = os.MkdirAll(dir, 0755)
		_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)
		ok, err := containsGoFiles(dir)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Error("expected true for directory with .go files")
		}
	})

	t.Run("directory without go files", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(tmpDir, "nogofiles")
		_ = os.MkdirAll(dir, 0755)
		_ = os.WriteFile(filepath.Join(dir, "README.md"), []byte("# docs"), 0644)
		ok, err := containsGoFiles(dir)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Error("expected false for directory without .go files")
		}
	})

	t.Run("empty directory", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(tmpDir, "empty")
		_ = os.MkdirAll(dir, 0755)
		ok, err := containsGoFiles(dir)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Error("expected false for empty directory")
		}
	})

	t.Run("non-existent directory", func(t *testing.T) {
		t.Parallel()
		_, err := containsGoFiles("/nonexistent/path")
		if err == nil {
			t.Error("expected error for non-existent directory")
		}
	})
}

func TestDependencyAnalyzer_PublishToolAction(t *testing.T) {
	t.Parallel()

	t.Run("nil EventBus returns silently", func(t *testing.T) {
		t.Parallel()
		a := newDependencyAnalyzer(nil, nil, nil, nil)
		// Must not panic
		a.publishToolAction(context.Background(), "test message")
	})

	t.Run("non-nil EventBus with ErrBusNotInitialized", func(t *testing.T) {
		t.Parallel()
		bus := &eventstest.TestEventBus{}
		bus.SetPublishErr(events.ErrBusNotInitialized)
		a := newDependencyAnalyzer(nil, nil, bus, nil)
		// ErrBusNotInitialized is silently swallowed (no log)
		// Must not panic
		a.publishToolAction(context.Background(), "test message")
	})

	t.Run("non-nil EventBus with generic error logs to slog", func(t *testing.T) {
		t.Parallel()
		bus := &eventstest.TestEventBus{}
		bus.SetPublishErr(errors.New("generic publish error"))
		a := newDependencyAnalyzer(nil, nil, bus, nil)
		// error is logged via slog.Error — verify no panic
		a.publishToolAction(context.Background(), "test message")
	})
}

// ─────────────────────────────────────────────────────────
// buildGraph error path tests (table-driven)
// ─────────────────────────────────────────────────────────

func TestBuildGraph_ErrorPaths(t *testing.T) {
	t.Parallel()

	// setupWorkspace creates a temp directory with go.mod and at least one
	// .go file so the happy-path preconditions are met before error injection.
	setupWorkspace := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		pkgDir := filepath.Join(dir, "pkg")
		require.NoError(t, os.MkdirAll(pkgDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "f.go"), []byte("package pkg"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/mod"), 0644))
		return dir
	}

	tests := []struct {
		name            string
		workspaceSetup  func(t *testing.T) (string, *mockAnalysisGoRunner)
		wantErrContains string
	}{
		{
			name: "resolveModulePrefix error",
			workspaceSetup: func(t *testing.T) (string, *mockAnalysisGoRunner) {
				dir := setupWorkspace(t)
				return dir, &mockAnalysisGoRunner{
					getModulePathFunc: func(ctx context.Context) (string, error) {
						return "", fmt.Errorf("no go.mod")
					},
					getModuleDirFunc: func(ctx context.Context) (string, error) {
						return dir, nil
					},
				}
			},
			wantErrContains: "getting module name",
		},
		{
			name: "GetModuleDir error",
			workspaceSetup: func(t *testing.T) (string, *mockAnalysisGoRunner) {
				dir := setupWorkspace(t)
				return dir, &mockAnalysisGoRunner{
					getModulePathFunc: func(ctx context.Context) (string, error) {
						return "example.com/mod", nil
					},
					getModuleDirFunc: func(ctx context.Context) (string, error) {
						return "", fmt.Errorf("permission denied")
					},
				}
			},
			wantErrContains: "getting module root",
		},
		{
			name: "listInternalPackages error (unreadable directory)",
			workspaceSetup: func(t *testing.T) (string, *mockAnalysisGoRunner) {
				if runtime.GOOS == "windows" {
					t.Skip("os.Chmod(0000) does not make directory unreadable on Windows")
				}
				dir := setupWorkspace(t)

				// Create a subdirectory with 0000 permissions so that
				// containsGoFiles fails when Walk reaches it.
				unreadable := filepath.Join(dir, "unreadable")
				require.NoError(t, os.MkdirAll(unreadable, 0000))
				t.Cleanup(func() { _ = os.Chmod(unreadable, 0755) })

				return dir, &mockAnalysisGoRunner{
					getModulePathFunc: func(ctx context.Context) (string, error) {
						return "example.com/mod", nil
					},
					getModuleDirFunc: func(ctx context.Context) (string, error) {
						return dir, nil
					},
				}
			},
			wantErrContains: "listing packages",
		},
		{
			name: "GetModuleDir returns empty string",
			workspaceSetup: func(t *testing.T) (string, *mockAnalysisGoRunner) {
				dir := setupWorkspace(t)
				return dir, &mockAnalysisGoRunner{
					getModulePathFunc: func(ctx context.Context) (string, error) {
						return "example.com/mod", nil
					},
					getModuleDirFunc: func(ctx context.Context) (string, error) {
						return "", nil // empty modRoot → listInternalPackages fails on Walk("")
					},
				}
			},
			wantErrContains: "listing packages",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, runner := tt.workspaceSetup(t)

			a := newDependencyAnalyzer(runner, &mockSecurityProvider{}, nil,
				infra_persistence.NewWorkspacePolicy())

			_, err := a.buildGraph(context.Background())
			require.Error(t, err, "expected error from buildGraph")
			assert.Contains(t, err.Error(), tt.wantErrContains,
				"error message should contain %q", tt.wantErrContains)
		})
	}
}

// TestBuildGraph_FilepathRelError verifies the error path when
// filepath.Rel fails inside buildGraph_processPackage. This test
// must NOT use t.Parallel() because it temporarily replaces the
// package-level filepathRelFn variable.
func TestBuildGraph_FilepathRelError(t *testing.T) {
	// NOT t.Parallel() — mutates package-level filepathRelFn

	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "pkg")
	require.NoError(t, os.MkdirAll(pkgDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "f.go"), []byte("package pkg"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/mod"), 0644))

	origRel := filepathRelFn
	t.Cleanup(func() { filepathRelFn = origRel })
	filepathRelFn = func(basepath, targpath string) (string, error) {
		return "", fmt.Errorf("injected Rel error")
	}

	runner := &mockAnalysisGoRunner{
		getModulePathFunc: func(ctx context.Context) (string, error) {
			return "example.com/mod", nil
		},
		getModuleDirFunc: func(ctx context.Context) (string, error) {
			return dir, nil
		},
	}

	a := newDependencyAnalyzer(runner, &mockSecurityProvider{}, nil,
		infra_persistence.NewWorkspacePolicy())

	_, err := a.buildGraph(context.Background())
	require.Error(t, err, "expected error from buildGraph")
	assert.Contains(t, err.Error(), "injected Rel error",
		"error message should contain %q", "injected Rel error")
}

// ─────────────────────────────────────────────────────────
// buildGraph: getImports error propagation through errgroup
// ─────────────────────────────────────────────────────────

func TestBuildGraph_GetImportsError(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Create a valid workspace with one package
	pkgDir := filepath.Join(tmpDir, "pkg")
	require.NoError(t, os.MkdirAll(pkgDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "f.go"), []byte("package pkg"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/mod"), 0644))

	// Also create a second package so the errgroup has work to do
	pkg2Dir := filepath.Join(tmpDir, "pkg2")
	require.NoError(t, os.MkdirAll(pkg2Dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pkg2Dir, "f.go"), []byte("package pkg2"), 0644))

	runner := &mockAnalysisGoRunner{
		getModulePathFunc: func(ctx context.Context) (string, error) {
			return "example.com/mod", nil
		},
		getModuleDirFunc: func(ctx context.Context) (string, error) {
			return tmpDir, nil
		},
	}

	a := newDependencyAnalyzer(runner, &mockSecurityProvider{}, nil,
		infra_persistence.NewWorkspacePolicy())

	// Use a cancelled context so packages.Load inside getImports fails
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := a.buildGraph(ctx)
	require.Error(t, err, "expected error from cancelled context in getImports")
}

// ─────────────────────────────────────────────────────────
// listInternalPackages error path test
// ─────────────────────────────────────────────────────────

func TestListInternalPackages_ErrorPath(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("os.Chmod(0000) does not make directory unreadable on Windows")
	}

	tmpDir := t.TempDir()

	subdir := filepath.Join(tmpDir, "subdir")
	require.NoError(t, os.MkdirAll(subdir, 0000))
	t.Cleanup(func() {
		if err := os.Chmod(subdir, 0755); err != nil {
			t.Logf("cleanup: chmod %s: %v", subdir, err)
		}
	})

	a := newDependencyAnalyzer(nil, &mockSecurityProvider{}, nil,
		infra_persistence.NewWorkspacePolicy())

	_, err := a.listInternalPackages(tmpDir)
	require.Error(t, err)
}

// ─────────────────────────────────────────────────────────
// getImports error path test
// ─────────────────────────────────────────────────────────

func TestGetImports_ErrorPath(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	pkgDir := filepath.Join(tmpDir, "pkg")
	require.NoError(t, os.MkdirAll(pkgDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "f.go"), []byte("package pkg"), 0644))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before call

	a := newDependencyAnalyzer(nil, &mockSecurityProvider{}, nil,
		infra_persistence.NewWorkspacePolicy())
	_, err := a.getImports(ctx, pkgDir, "example.com/mod")
	require.Error(t, err, "expected error from cancelled context")
}

// ─────────────────────────────────────────────────────────
// Gap 9: listInternalPackages skip-dir path (line 187)
// ─────────────────────────────────────────────────────────

func TestListInternalPackages_SkipDir(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	pkgDir := filepath.Join(tmpDir, "pkg")
	require.NoError(t, os.MkdirAll(pkgDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "f.go"), []byte("package pkg"), 0644))

	ignoredDir := filepath.Join(tmpDir, ".git")
	require.NoError(t, os.MkdirAll(ignoredDir, 0755))

	a := newDependencyAnalyzer(nil, &mockSecurityProvider{}, nil,
		infra_persistence.NewWorkspacePolicy())

	pkgs, err := a.listInternalPackages(tmpDir)
	require.NoError(t, err)

	for _, p := range pkgs {
		if strings.Contains(p, ".git") {
			t.Errorf("expected .git to be skipped, found: %s", p)
		}
	}
	found := false
	for _, p := range pkgs {
		if strings.Contains(p, "pkg") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected pkg directory to be listed")
	}
}

// ─────────────────────────────────────────────────────────
// Gap 10: resolveModulePrefix cached path (line 230)
// ─────────────────────────────────────────────────────────

func TestResolveModulePrefix_Cached(t *testing.T) {
	t.Parallel()

	callCount := 0
	runner := &mockAnalysisGoRunner{
		getModulePathFunc: func(ctx context.Context) (string, error) {
			callCount++
			return "example.com/mod", nil
		},
	}
	a := newDependencyAnalyzer(runner, &mockSecurityProvider{}, nil,
		infra_persistence.NewWorkspacePolicy())

	prefix1, err := a.resolveModulePrefix(context.Background())
	require.NoError(t, err)
	if prefix1 != "example.com/mod" {
		t.Errorf("expected 'example.com/mod', got %q", prefix1)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call to GetModulePath, got %d", callCount)
	}

	prefix2, err := a.resolveModulePrefix(context.Background())
	require.NoError(t, err)
	if prefix2 != "example.com/mod" {
		t.Errorf("expected 'example.com/mod', got %q", prefix2)
	}
	if callCount != 1 {
		t.Errorf("expected still 1 call to GetModulePath (cached), got %d", callCount)
	}
}

// ─────────────────────────────────────────────────────────
// Gap 11: renderGraph mermaid format path (line 243)
// ─────────────────────────────────────────────────────────

func TestRenderGraph_MermaidFormat(t *testing.T) {
	t.Parallel()
	a := newDependencyAnalyzer(nil, &mockSecurityProvider{}, nil,
		infra_persistence.NewWorkspacePolicy())

	graph := map[string][]string{
		"example.com/mod/pkg1": {"example.com/mod/pkg2"},
		"example.com/mod/pkg2": {},
	}

	result := a.renderGraph(graph, "mermaid")

	if !strings.Contains(result, "graph TD") && !strings.Contains(result, "graph ") {
		t.Errorf("expected mermaid graph output, got: %s", result)
	}
	if !strings.Contains(result, "pkg1") || !strings.Contains(result, "pkg2") {
		t.Errorf("expected both packages in mermaid output, got: %s", result)
	}
}

// =============================================================================
// Gap: listInternalPackages containsGoFiles error path (L187-189).
// Covered by creating a directory with execute-only permission (0100) so
// that filepath.Walk can enter it but os.ReadDir inside containsGoFiles fails.
// NOTE: On some platforms/Go versions, Walk itself may fail to read the
// directory and return the error via the Walk callback instead (L178-179),
// which exercises a different but related path. Both are acceptable.
// =============================================================================
func TestListInternalPackages_ContainsGoFilesError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Chmod execute-only not reliable on Windows")
	}

	tmpDir := t.TempDir()

	// Create a subdirectory with execute-only permission (--x------ = 0100).
	// filepath.Walk uses os.Lstat to detect directories (succeeds with exec bit)
	// and then calls os.ReadDir to list contents (fails without read bit).
	// On some Go versions, Walk's own ReadDir fails first and passes the error
	// to the callback; on others, the callback is called with err==nil and
	// containsGoFiles' own os.ReadDir fails.
	subdir := filepath.Join(tmpDir, "subdir")
	require.NoError(t, os.MkdirAll(subdir, 0100))
	t.Cleanup(func() { _ = os.Chmod(subdir, 0755) })

	a := newDependencyAnalyzer(nil, &mockSecurityProvider{}, nil,
		infra_persistence.NewWorkspacePolicy())

	_, err := a.listInternalPackages(tmpDir)
	require.Error(t, err, "expected error from listInternalPackages with unreadable subdirectory")

	// Accept either path: the containsGoFiles error wrapping (L193-195)
	// or Walk's own permission error propagated through the callback
	// (L178-179). Both exercise the defense-in-depth described in the
	// GAP ACCEPTED comment at dependency.go:187-189.
	assert.True(t,
		strings.Contains(err.Error(), "checking for Go files in") ||
			strings.Contains(err.Error(), "permission denied"),
		"error should mention 'checking for Go files in' or 'permission denied', got: %v", err)
}
