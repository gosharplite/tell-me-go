package analysis

import (
	"bytes"
	"context"
	"errors"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIndexer_SymbolClassification(t *testing.T) {
	t.Parallel()
	code := `package test
type MyStruct struct{}
func (s *MyStruct) MyPointerMethod() {}
func (s MyStruct) MyValueMethod() {}
type MyInterface interface { M() }
func MyFunc() {}
var MyVar = 1
const MyConst = 2
type MyAlias = int
func unexported() {}
`
	tmpDir, idx := setupIndexerWorkspace(t, code)
	ctx := context.Background()

	tests := []struct {
		name     string
		kind     string
		receiver string
	}{
		{"MyPointerMethod", "func", "*MyStruct"},
		{"MyValueMethod", "func", "MyStruct"},
		{"MyFunc", "func", ""},
		{"MyVar", "var", ""},
		{"MyConst", "const", ""},
		{"MyStruct", "type", ""},
		{"MyAlias", "type", ""},
		{"MyInterface", "type", ""},
		{"unexported", "func", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			syms, err := idx.SearchSymbols(ctx, tmpDir, tt.name, false, nil)
			require.NoError(t, err)

			var found *symbolLocation
			for i := range syms {
				if syms[i].Name == tt.name {
					found = &syms[i]
					break
				}
			}

			require.NotNil(t, found, "symbol %s not found", tt.name)
			assert.Equal(t, tt.kind, found.Kind)
			assert.Equal(t, tt.receiver, found.Receiver)
		})
	}
}

func setupIndexerWorkspace(t *testing.T, code string) (string, *indexer) {
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.25"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte(code), 0644))

	idx, err := newIndexer(tmpDir)
	require.NoError(t, err)

	require.NoError(t, idx.Refresh(context.Background(), nil))

	return tmpDir, idx
}

func TestIndexer_FindImplementors(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.25"), 0644)
	code := `package test
type I interface { M() }
type S struct{}
func (s S) M() {}
`
	_ = os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte(code), 0644)

	idx, _ := newIndexer(tmpDir)
	ctx := context.Background()
	_ = idx.Refresh(ctx, nil)

	implementors, err := idx.FindImplementors(ctx, "I", nil)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, imp := range implementors {
		if imp.Name == "S" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected S to implement I")
	}
}

func TestIndexer_Lookup(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.25"), 0644)
	code := `package test
type T struct{}
`
	_ = os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte(code), 0644)

	idx, _ := newIndexer(tmpDir)
	ctx := context.Background()
	_ = idx.Refresh(ctx, nil)

	locs, err := idx.Lookup(ctx, "T", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(locs) == 0 {
		t.Fatal("Expected to find location of T")
	}
	if !strings.HasSuffix(locs[0].Path, "test.go") {
		t.Errorf("Expected path to end with test.go, got %s", locs[0].Path)
	}
}

func TestIndexer_ErrorPersistence(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.25"), 0644)

	// Valid file
	_ = os.WriteFile(filepath.Join(tmpDir, "valid.go"), []byte("package test\nfunc F() {}"), 0644)

	idx, _ := newIndexer(tmpDir)
	ctx := context.Background()
	_ = idx.Refresh(ctx, nil)

	syms, _ := idx.SearchSymbols(ctx, tmpDir, "F", false, nil)
	if len(syms) != 1 {
		t.Fatal("Expected to find F")
	}

	// Create a file with syntax error
	_ = os.WriteFile(filepath.Join(tmpDir, "invalid.go"), []byte("package test\nfunc G() {"), 0644)

	// Force refresh by resetting lastRefresh
	idx.mu.Lock()
	idx.lastRefresh = time.Time{}
	idx.mu.Unlock()

	_ = idx.Refresh(ctx, nil)

	syms, _ = idx.SearchSymbols(ctx, tmpDir, "F", false, nil)
	if len(syms) == 0 {
		t.Error("Expected to still have F in despite other file errors")
	}
}

func TestToLocation_AbsFailure(t *testing.T) {
	t.Parallel()

	idx := &indexer{
		fset: token.NewFileSet(),
		resolvePath: func(s string) (string, error) {
			return "", errors.New("injected resolvePath failure")
		},
	}
	f := idx.fset.AddFile("some/path/test.go", -1, 100)
	loc := idx.toLocation(f.Pos(0))

	assert.NotEmpty(t, loc.Path, "Path must not be empty even when resolvePath fails")
	assert.Equal(t, "some/path/test.go", loc.Path, "Path must fall back to raw filename")
}

func TestRefresh_LoadPackagesErrorWrapping(t *testing.T) {
	t.Parallel()

	idx, err := newIndexer("/nonexistent/path/that/does/not/exist")
	require.NoError(t, err)

	idx.mu.Lock()
	idx.lastRefresh = time.Time{}
	idx.mu.Unlock()

	err = idx.Refresh(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading packages:")
}

func TestDiscoverModulePath_LoadFails(t *testing.T) {
	// Not parallel: uses global log.SetOutput

	idx, err := newIndexer("/nonexistent/for/discover/module")
	require.NoError(t, err)

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	result := idx.discoverModulePath(context.Background(), token.NewFileSet())

	assert.Equal(t, "", result)
	assert.Contains(t, buf.String(), "discoverModulePath failed")
}

func TestIsSymbolUsed_RefreshFails(t *testing.T) {
	// Not parallel: uses global log.SetOutput

	idx := &indexer{
		dir:         "/nonexistent/for/is/symbol/used",
		fset:        token.NewFileSet(),
		resolvePath: filepath.Abs,
	}

	idx.mu.Lock()
	idx.lastRefresh = time.Time{}
	idx.mu.Unlock()

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	result := idx.IsSymbolUsed(context.Background(), "Anything", nil)

	assert.False(t, result)
	assert.Contains(t, buf.String(), "analysis: index refresh failed in IsSymbolUsed")
}

func TestLookup_RefreshFails(t *testing.T) {
	t.Parallel()

	idx := &indexer{
		dir:           "/nonexistent/for/lookup",
		fset:          token.NewFileSet(),
		resolvePath:   filepath.Abs,
		implsCache:    &implCacheEntry{},
		symbolsByPath: make(map[string][]symbolLocation),
		usagesByName:  make(map[string][]location),
	}

	ctx := context.Background()
	result, err := idx.Lookup(ctx, "Anything", nil)
	if err == nil {
		t.Fatal("expected error from Lookup with non-existent dir, got nil")
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
	assert.Contains(t, err.Error(), "refreshing index for lookup")
}

func TestPackages_RefreshFails(t *testing.T) {
	t.Parallel()

	idx := &indexer{
		dir:           "/nonexistent/for/packages",
		fset:          token.NewFileSet(),
		resolvePath:   filepath.Abs,
		implsCache:    &implCacheEntry{},
		symbolsByPath: make(map[string][]symbolLocation),
		usagesByName:  make(map[string][]location),
	}

	ctx := context.Background()
	result, err := idx.Packages(ctx, nil)
	if err == nil {
		t.Fatal("expected error from Packages with non-existent dir, got nil")
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
	assert.Contains(t, err.Error(), "refreshing index for packages")
}

func TestStartHeartbeatTicker_NonNilHb(t *testing.T) {
	// startHeartbeatTicker with non-nil hb exercises the hb send path.
	hb := make(chan struct{}, 10)
	stop := startHeartbeatTicker(hb)

	// Wait for at least one tick to arrive
	select {
	case <-hb:
		// Success: heartbeat received
	case <-time.After(5 * time.Second):
		t.Fatal("expected at least one heartbeat within 5 seconds")
	}

	// Stop the ticker and verify the channel stops producing
	stop()

	// Drain the channel
	drained := false
	for !drained {
		select {
		case <-hb:
		case <-time.After(100 * time.Millisecond):
			drained = true
		}
	}

	// After stopping and draining, no more heartbeats should arrive
	select {
	case <-hb:
		t.Error("unexpected heartbeat after stop")
	case <-time.After(3 * time.Second):
		// Expected: no heartbeat
	}
}

func TestStartHeartbeatTicker_HbFull(t *testing.T) {
	// When the hb channel is full, the ticker drops the heartbeat (default case).
	// This test verifies that a full channel does not block the ticker goroutine.
	hb := make(chan struct{}, 1)
	// Fill the channel
	hb <- struct{}{}

	stop := startHeartbeatTicker(hb)

	// The ticker should not block even though the channel is full.
	// Wait briefly and verify the goroutine is still alive by stopping it.
	time.Sleep(3 * time.Second)
	stop() // Must not hang

	// Drain
	select {
	case <-hb:
	default:
	}
}

func TestRefresh_DoubleCheckFails_NoRepeatedLoad(t *testing.T) {
	t.Parallel()

	// First refresh sets lastRefresh to now
	_, idx := setupIndexerWorkspace(t, "package test\nfunc F() {}")

	// needsRefresh is false → Refresh returns nil without re-loading
	err := idx.Refresh(context.Background(), nil)
	require.NoError(t, err, "second Refresh (within TTL) must return nil without error")
}

func TestRefresh_HarvestPackagesError(t *testing.T) {
	t.Parallel()

	// Create a minimal workspace so loadPackages succeeds, but inject a
	// harvestPackages failure via a resolvePath that fails for every path.
	// harvestPackages calls processPackage → processFile → resolvePath,
	// so if resolvePath always fails, processFile will fail and the
	// errgroup will propagate the error.
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.25"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte("package test\nfunc F() {}"), 0644))

	idx, err := newIndexer(tmpDir)
	require.NoError(t, err)

	// Replace resolvePath with one that always fails
	idx.resolvePath = func(s string) (string, error) {
		return "", errors.New("injected resolvePath failure for harvest test")
	}

	// Force refresh
	idx.mu.Lock()
	idx.lastRefresh = time.Time{}
	idx.mu.Unlock()

	err = idx.Refresh(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "harvesting packages")
}
