// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
)

type failingWriteFS struct {
	persistence.FileSystem
	failWrite bool
}

func (m *failingWriteFS) WriteFile(ctx context.Context, name string, data []byte, perm os.FileMode) error {
	if m.failWrite && filepath.Ext(name) == ".jsonl" {
		return errors.New("disk full")
	}
	return m.FileSystem.WriteFile(ctx, name, data, perm)
}

func TestJSONLStore_Migration_Failure(t *testing.T) {
	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, "history.json")
	jsonlPath := filepath.Join(tmpDir, "history.jsonl")

	// 1. Create legacy history.json
	content := `[{"Role":"user", "Parts":[{"Text":"important data"}]}]`
	if err := os.WriteFile(jsonPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// 2. Initialize store with failing filesystem
	ffs := &failingWriteFS{FileSystem: infrapersistence.NewOSFileSystem(), failWrite: true}
	store := newJSONLStoreWithAssetStore(ffs, persistencetest.NewAssetStore(ffs, filepath.Join(filepath.Dir(jsonlPath), "assets")), jsonlPath, filepath.Join(tmpDir, "history.archive.jsonl"))
	ctx := context.Background()

	// 3. Load should attempt migration but fail to write .jsonl
	contents, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// 4. Verify data is NOT lost (it should fallback to .json)
	if len(contents) != 1 {
		t.Fatalf("expected 1 entry, got %d. Data might have been lost!", len(contents))
	}
	if contents[0].Parts[0].Text != "important data" {
		t.Errorf("expected 'important data', got %q", contents[0].Parts[0].Text)
	}

	// 5. Verify history.json STILL exists
	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		t.Error("legacy history.json was deleted even though migration failed!")
	}

	// 6. Verify history.jsonl does NOT exist (or is empty/failed)
	if _, err := os.Stat(jsonlPath); err == nil {
		t.Error("history.jsonl should not exist if migration failed")
	}
}
