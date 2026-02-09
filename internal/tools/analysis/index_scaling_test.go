package analysis

import (
	"context"
	"testing"
	"time"
)

func TestIndexer_Scaling(t *testing.T) {
	// Use current directory for testing
	idx, err := NewIndexer(".")
	if err != nil {
		t.Fatalf("failed to create r: %v", err)
	}
	ctx := context.Background()

	// 1. Initial refresh (O(N) scan)
	err = idx.Refresh(ctx)
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

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
		if sym.Name == "Indexer" && sym.Kind == "type" {
			foundIndexer = true
			break
		}
	}

	if !foundIndexer {
		t.Errorf("Expected to find 'Indexer' type in symbols, got %d symbols", len(symbols))
	}
}

func TestIndexer_GetUsages_Scaling(t *testing.T) {
	idx, err := NewIndexer(".")
	if err != nil {
		t.Fatalf("failed to create r: %v", err)
	}
	ctx := context.Background()
	if err := idx.Refresh(ctx); err != nil {
		t.Fatalf("failed to refresh  %v", err)
	}

	start := time.Now()
	usages, err := idx.GetUsages(ctx, "Indexer", ".")
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("GetUsages failed: %v", err)
	}

	if duration > 10*time.Millisecond {
		t.Errorf("Usage lookup too slow (%v), likely performing FS walk", duration)
	}

	if len(usages) == 0 {
		t.Error("Expected to find usages of 'Indexer'")
	}
}
