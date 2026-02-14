package analysis

import (
	"context"
	"testing"
	"time"
)

func TestIndexer_Scaling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping scaling test in short mode")
	}
	t.Parallel()
	// Use current directory for testing
	idx := getSharedIndexer(t)
	ctx := context.Background()

	// 2. Performance Check: SearchSymbols should be O(1) in-memory
	start := time.Now()
	symbols, err := idx.SearchSymbols(ctx, ".", "", false)
	duration := time.Since(start)
	t.Logf("SearchSymbols took %v", duration)

	if err != nil {
		t.Fatalf("SearchSymbols failed: %v", err)
	}

	if duration > 10*time.Millisecond {
		t.Errorf("Index lookup too slow (%v), likely performing FS walk", duration)
	}

	// 3. Verify consistency
	foundIndexer := false
	for _, sym := range symbols {
		if sym.Name == "indexer" && sym.Kind == "type" {
			foundIndexer = true
			break
		}
	}

	if !foundIndexer {
		t.Errorf("Expected to find 'indexer' type in symbols, got %d symbols", len(symbols))
	}
}

func TestIndexer_GetUsages_Scaling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping scaling test in short mode")
	}
	t.Parallel()
	idx := getSharedIndexer(t)
	ctx := context.Background()

	start := time.Now()
	usages, err := idx.GetUsages(ctx, "indexer", ".")
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("GetUsages failed: %v", err)
	}

	if duration > 10*time.Millisecond {
		t.Errorf("Usage lookup too slow (%v), likely performing FS walk", duration)
	}

	if len(usages) == 0 {
		t.Error("Expected to find usages of 'indexer'")
	}
}
