package index

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIndexer_Refresh_ContextCancel(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.24"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte("package test\nfunc F() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	idx, _ := NewIndexer(tmpDir)

	// Use a context that cancels very quickly
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	err := idx.Refresh(ctx)
	if err == nil {
		t.Log("Refresh finished before cancellation, which is fine")
	} else if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("expected context error, got: %v", err)
	}
}
