// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
)

// BenchmarkManagerAddContent measures the end-to-end latency of Manager.AddContent
// including the lock, clone, store.Append, and in-memory append against
// a Manager pre-seeded with different amounts of history.
func BenchmarkManagerAddContent(b *testing.B) {
	preSeedSizes := []int{0, 1000}
	for _, preSeed := range preSeedSizes {
		b.Run(fmt.Sprintf("preSeed=%d", preSeed), func(b *testing.B) {
			ctx := context.Background()
			tmpDir := b.TempDir()
			fs := persistencetest.NewPlainOSFileSystem()
			filePath := filepath.Join(tmpDir, "history.jsonl")
			archivePath := filepath.Join(tmpDir, "archive.jsonl")
			m := NewManager(fs, filePath, archivePath)

			if preSeed > 0 {
				if err := m.SetContents(ctx, generateContents(preSeed)); err != nil {
					b.Fatalf("SetContents seed failed: %v", err)
				}
			}

			entry := &llm.Content{
				Role: "user",
				Parts: []*llm.Part{
					{Text: "benchmark append message"},
				},
			}

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = m.AddContent(ctx, entry)
			}
		})
	}
}

// BenchmarkManagerSave measures the end-to-end latency of Manager.Save
// including the lock, store.Save, and store.Sync (fsync) overhead
// against a Manager pre-loaded with the given number of content entries.
func BenchmarkManagerSave(b *testing.B) {
	sizes := []int{100, 1000, 5000}
	for _, size := range sizes {
		b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
			ctx := context.Background()
			tmpDir := b.TempDir()
			fs := persistencetest.NewPlainOSFileSystem()
			filePath := filepath.Join(tmpDir, "history.jsonl")
			archivePath := filepath.Join(tmpDir, "archive.jsonl")
			m := NewManager(fs, filePath, archivePath)

			if err := m.SetContents(ctx, generateContents(size)); err != nil {
				b.Fatalf("SetContents seed failed: %v", err)
			}

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = m.Save(ctx)
			}
		})
	}
}
