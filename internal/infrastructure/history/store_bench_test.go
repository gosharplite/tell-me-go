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

// generateContents creates n Content fixtures with alternating roles
// (user for even indices, model for odd) and a single text Part.
func generateContents(n int) []*llm.Content {
	contents := make([]*llm.Content, n)
	for i := 0; i < n; i++ {
		role := "user"
		if i%2 != 0 {
			role = "model"
		}
		contents[i] = &llm.Content{
			Role: role,
			Parts: []*llm.Part{
				{Text: fmt.Sprintf("Message %d: The quick brown fox jumps over the lazy dog.", i)},
			},
		}
	}
	return contents
}

// generateContentsWithInlineData creates n Content fixtures with alternating
// roles, a text Part, and an additional InlineData Part that exercises the
// prepareForStorage → AssetStore.Put path during Save.
func generateContentsWithInlineData(n int) []*llm.Content {
	contents := make([]*llm.Content, n)
	for i := 0; i < n; i++ {
		role := "user"
		if i%2 != 0 {
			role = "model"
		}
		contents[i] = &llm.Content{
			Role: role,
			Parts: []*llm.Part{
				{Text: fmt.Sprintf("Message %d: The quick brown fox jumps over the lazy dog.", i)},
				{InlineData: &llm.Blob{
					MIMEType: "image/png",
					Data:     []byte(fmt.Sprintf("fake-image-data-%d", i)),
				}},
			},
		}
	}
	return contents
}

// BenchmarkJSONLStoreAppend measures incremental append latency of jsonlStore.Append
// simulating turn-by-turn writes against files pre-seeded with different amounts of history.
func BenchmarkJSONLStoreAppend(b *testing.B) {
	preSeedSizes := []int{0, 1000, 5000}
	for _, preSeed := range preSeedSizes {
		b.Run(fmt.Sprintf("preSeed=%d", preSeed), func(b *testing.B) {
			ctx := context.Background()
			tmpDir := b.TempDir()
			fs := persistencetest.NewPlainOSFileSystem()
			filePath := filepath.Join(tmpDir, "history.jsonl")
			archivePath := filepath.Join(tmpDir, "archive.jsonl")

			singleEntry := []*llm.Content{{
				Role: "user",
				Parts: []*llm.Part{
					{Text: "Appended turn message"},
				},
			}}

			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				store := newJSONLStore(fs, filePath, archivePath)
				if preSeed > 0 {
					seed := generateContents(preSeed)
					if err := store.Save(ctx, seed); err != nil {
						b.Fatalf("seed Save failed: %v", err)
					}
				}
				b.StartTimer()

				_ = store.Append(ctx, singleEntry)
			}
		})
	}
}

// BenchmarkJSONLStoreCompact measures the full compaction cycle (Load → Save)
// against a file pre-seeded with content entries that already have metadata set.
func BenchmarkJSONLStoreCompact(b *testing.B) {
	sizes := []int{100, 5000}
	for _, size := range sizes {
		b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
			ctx := context.Background()
			tmpDir := b.TempDir()
			fs := persistencetest.NewPlainOSFileSystem()
			filePath := filepath.Join(tmpDir, "history.jsonl")
			archivePath := filepath.Join(tmpDir, "archive.jsonl")

			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()

				seed := generateContents(size)
				for idx := range seed {
					seed[idx].Pinned = true
				}

				store := newJSONLStore(fs, filePath, archivePath)
				if err := store.Save(ctx, seed); err != nil {
					b.Fatalf("seed Save failed: %v", err)
				}

				b.StartTimer()

				_ = store.Compact(ctx)
			}
		})
	}
}

// BenchmarkJSONLStoreSave measures compaction write throughput of jsonlStore.Save
// for both text-only content and content with inline binary data.
func BenchmarkJSONLStoreSave(b *testing.B) {
	textSizes := []int{100, 1000, 5000}
	for _, size := range textSizes {
		b.Run(fmt.Sprintf("TextOnly/size=%d", size), func(b *testing.B) {
			ctx := context.Background()
			tmpDir := b.TempDir()
			fs := persistencetest.NewPlainOSFileSystem()
			filePath := filepath.Join(tmpDir, "history.jsonl")
			archivePath := filepath.Join(tmpDir, "archive.jsonl")
			fixture := generateContents(size)
			store := newJSONLStore(fs, filePath, archivePath)

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = store.Save(ctx, fixture)
			}
		})
	}

	inlineSizes := []int{100, 1000}
	for _, size := range inlineSizes {
		b.Run(fmt.Sprintf("WithInlineData/size=%d", size), func(b *testing.B) {
			ctx := context.Background()
			tmpDir := b.TempDir()
			fs := persistencetest.NewPlainOSFileSystem()
			filePath := filepath.Join(tmpDir, "history.jsonl")
			archivePath := filepath.Join(tmpDir, "archive.jsonl")
			fixture := generateContentsWithInlineData(size)
			store := newJSONLStore(fs, filePath, archivePath)

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = store.Save(ctx, fixture)
			}
		})
	}
}
