package analysis

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestIndexer_Concurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrency test in short mode")
	}
	t.Parallel()
	tmpDir, idx, ctx := setupIndexerConcurrency(t)

	start := make(chan struct{})

	t.Run("ParallelSearchAndRefresh", func(t *testing.T) {
		t.Parallel()
		var wg sync.WaitGroup

		// 10 goroutines doing SearchSymbols
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				runSearchLoop(ctx, idx, tmpDir, 100, t)
			}()
		}

		// 2 goroutines doing Refresh frequently
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				runRefreshLoop(ctx, idx, 10, t)
			}()
		}

		close(start)
		wg.Wait()
	})
}

func setupIndexerConcurrency(t *testing.T) (string, *indexer, context.Context) {
	tmpDir := t.TempDir()

	// Create a dummy project
	err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.25"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	code := `package test
func F1() {}
func F2() {}
`
	err = os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte(code), 0644)
	if err != nil {
		t.Fatal(err)
	}

	idx, err := newIndexer(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Pre-warm the index
	if err := idx.Refresh(ctx, nil); err != nil {
		t.Fatal(err)
	}
	return tmpDir, idx, ctx
}

func runSearchLoop(ctx context.Context, idx *indexer, tmpDir string, iterations int, t *testing.T) {
	for j := 0; j < iterations; j++ {
		syms, err := idx.SearchSymbols(ctx, tmpDir, "F1", false, nil)
		if err != nil {
			t.Errorf("SearchSymbols error: %v", err)
		}
		if len(syms) == 0 {
			t.Errorf("expected to find F1, got 0 symbols (mid-refresh empty result?)")
		}
	}
}

func runRefreshLoop(ctx context.Context, idx *indexer, iterations int, t *testing.T) {
	for j := 0; j < iterations; j++ {
		if err := idx.Refresh(ctx, nil); err != nil {
			t.Errorf("Refresh error: %v", err)
		}
	}
}
