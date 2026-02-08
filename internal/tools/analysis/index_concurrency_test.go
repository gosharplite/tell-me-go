package analysis

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestIndexer_Concurrency(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a dummy project
	err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.24"), 0644)
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

	idx, err := NewIndexer(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Pre-warm the index
	if err := idx.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	start := make(chan struct{})

	// 10 goroutines doing SearchSymbols
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 100; j++ {
				syms, err := idx.SearchSymbols(ctx, tmpDir, "F1", false)
				if err != nil {
					t.Errorf("SearchSymbols error: %v", err)
				}
				if len(syms) == 0 {
					t.Errorf("expected to find F1, got 0 symbols (mid-refresh empty result?)")
				}
			}
		}()
	}

	// 2 goroutines doing Refresh frequently (forcing TTL bypass if we could,
	// but here we just test that they don't crash or return empty results)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 10; j++ {
				// We don't wait for TTL here to test concurrency safety,
				// though Refresh will actually skip if TTL not met.
				// To force a refresh, we'd need to manipulate lastRefresh or wait.
				// For race testing, even the skipping path is valuable.
				if err := idx.Refresh(ctx); err != nil {
					t.Errorf("Refresh error: %v", err)
				}
				time.Sleep(10 * time.Millisecond)
			}
		}()
	}

	close(start)
	wg.Wait()
}
