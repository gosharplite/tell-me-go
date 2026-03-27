// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

func TestJSONLArchiveReader_ReadPage(t *testing.T) {
	ctx := context.Background()
	fs := persistence.NewOSFileSystem()
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "archive.jsonl")

	contents := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "Hello 1"}}},
		{Role: "assistant", Parts: []*llm.Part{{Text: "Response 1"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "Hello 2"}}},
		{Role: "assistant", Parts: []*llm.Part{{Text: "Response 2"}}},
	}

	// Create a dummy JSONL file
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	offsets := make([]int64, len(contents))
	var currentOffset int64
	for i, c := range contents {
		data, _ := json.Marshal(c)
		data = append(data, '\n')
		n, _ := f.Write(data)
		offsets[i] = currentOffset
		currentOffset += int64(n)
	}
	_ = f.Close()

	reader := history.NewJSONLArchiveReader(fs, archivePath)

	t.Run("read first page", func(t *testing.T) {
		dtos, nextOffset, err := reader.ReadPage(ctx, 2, 0)
		if err != nil {
			t.Fatalf("failed to read first page: %v", err)
		}
		if len(dtos) != 2 {
			t.Fatalf("expected 2 DTOs, got %d", len(dtos))
		}
		if dtos[0].ContentPreview != "Hello 1" || dtos[1].ContentPreview != "Response 1" {
			t.Errorf("unexpected DTO content: %+v", dtos)
		}
		if nextOffset != offsets[2] {
			t.Errorf("expected offset %d, got %d", offsets[2], nextOffset)
		}
	})

	t.Run("read second page from offset", func(t *testing.T) {
		dtos, _, err := reader.ReadPage(ctx, 2, offsets[2])
		if err != nil {
			t.Fatalf("failed to read second page: %v", err)
		}
		if len(dtos) != 2 {
			t.Fatalf("expected 2 DTOs, got %d", len(dtos))
		}
		if dtos[0].ContentPreview != "Hello 2" || dtos[1].ContentPreview != "Response 2" {
			t.Errorf("unexpected DTO content: %+v", dtos)
		}
	})

	t.Run("masking large binary data", func(t *testing.T) {
		// Append a content with binary data
		c := &llm.Content{
			Role: "user",
			Parts: []*llm.Part{
				{Text: "A picture:"},
				{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("fake binary data")}},
			},
		}
		f, _ := os.OpenFile(archivePath, os.O_APPEND|os.O_WRONLY, 0644)
		data, _ := json.Marshal(c)
		data = append(data, '\n')
		_, _ = f.Write(data)
		_ = f.Close()

		dtos, _, err := reader.ReadPage(ctx, 1, currentOffset)
		if err != nil {
			t.Fatalf("failed to read binary content: %v", err)
		}
		if len(dtos) != 1 {
			t.Fatalf("expected 1 DTO, got %d", len(dtos))
		}
		if !strings.Contains(dtos[0].ContentPreview, "[Attached Image]") {
			t.Errorf("expected masked image, got: %s", dtos[0].ContentPreview)
		}
	})
}

func BenchmarkReadPage(b *testing.B) {
	ctx := context.Background()
	fs := persistence.NewOSFileSystem()
	tmpDir := b.TempDir()
	archivePath := filepath.Join(tmpDir, "archive.jsonl")

	// Generate ~50MB file
	// Each line is ~1KB
	line := `{"role":"user","parts":[{"text":"` + strings.Repeat("A", 1000) + `"}]}` + "\n"
	lineBytes := []byte(line)
	lineCount := 50000

	f, err := os.Create(archivePath)
	if err != nil {
		b.Fatalf("failed to create benchmark file: %v", err)
	}
	for i := 0; i < lineCount; i++ {
		if _, err := f.Write(lineBytes); err != nil {
			b.Fatalf("failed to write to benchmark file: %v", err)
		}
	}
	_ = f.Close()

	reader := history.NewJSONLArchiveReader(fs, archivePath)

	// Jump to the middle
	offset := int64(25000 * len(lineBytes))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		dtos, _, err := reader.ReadPage(ctx, 10, offset)
		if err != nil || len(dtos) != 10 {
			b.Fatalf("failed to read page: %v (len=%d)", err, len(dtos))
		}
	}
}

func TestJSONLArchiveReader_ReadPage_NonExistent(t *testing.T) {
	ctx := context.Background()
	fs := persistence.NewOSFileSystem()
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "non_existent.jsonl")

	reader := history.NewJSONLArchiveReader(fs, archivePath)

	dtos, nextOffset, err := reader.ReadPage(ctx, 10, 0)
	if err != nil {
		t.Fatalf("expected no error for non-existent file, got %v", err)
	}
	if len(dtos) != 0 {
		t.Errorf("expected 0 DTOs, got %d", len(dtos))
	}
	if nextOffset != 0 {
		t.Errorf("expected nextOffset 0, got %d", nextOffset)
	}
}

func TestJSONLArchiveReader_ReadPrevious(t *testing.T) {
	ctx := context.Background()
	fs := persistence.NewOSFileSystem()
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "archive_prev.jsonl")

	contents := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "Msg 1"}}},
		{Role: "assistant", Parts: []*llm.Part{{Text: "Msg 2"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "Msg 3"}}},
		{Role: "assistant", Parts: []*llm.Part{{Text: "Msg 4"}}},
	}

	f, _ := os.Create(archivePath)
	offsets := make([]int64, len(contents))
	var currentOffset int64
	for i, c := range contents {
		data, _ := json.Marshal(c)
		data = append(data, '\n')
		n, _ := f.Write(data)
		offsets[i] = currentOffset
		currentOffset += int64(n)
	}
	_ = f.Close()

	reader := history.NewJSONLArchiveReader(fs, archivePath)

	t.Run("read last page (limit 2)", func(t *testing.T) {
		dtos, nextOffset, err := reader.ReadPrevious(ctx, 2, -1)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}
		if len(dtos) != 2 {
			t.Fatalf("expected 2, got %d", len(dtos))
		}
		if dtos[0].ContentPreview != "Msg 3" || dtos[1].ContentPreview != "Msg 4" {
			t.Errorf("unexpected content: %+v", dtos)
		}
		if nextOffset != offsets[2] {
			t.Errorf("expected offset %d, got %d", offsets[2], nextOffset)
		}
	})

	t.Run("read previous page from offset", func(t *testing.T) {
		dtos, nextOffset, err := reader.ReadPrevious(ctx, 2, offsets[2])
		if err != nil {
			t.Fatalf("failed: %v", err)
		}
		if len(dtos) != 2 {
			t.Fatalf("expected 2, got %d", len(dtos))
		}
		if dtos[0].ContentPreview != "Msg 1" || dtos[1].ContentPreview != "Msg 2" {
			t.Errorf("unexpected content: %+v", dtos)
		}
		if nextOffset != 0 {
			t.Errorf("expected offset 0, got %d", nextOffset)
		}
	})

	t.Run("read with limit larger than available", func(t *testing.T) {
		dtos, nextOffset, err := reader.ReadPrevious(ctx, 10, offsets[2])
		if err != nil {
			t.Fatalf("failed: %v", err)
		}
		if len(dtos) != 2 {
			t.Fatalf("expected 2, got %d", len(dtos))
		}
		if nextOffset != 0 {
			t.Errorf("expected offset 0, got %d", nextOffset)
		}
	})
}
