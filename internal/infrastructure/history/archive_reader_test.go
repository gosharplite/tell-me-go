// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
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

	type testCase struct {
		name           string
		limit          int
		offset         int64
		setup          func()
		expectedLen    int
		expectedNext   int64
		expectedFirst  string
		expectedSecond string
		validate       func(t *testing.T, dtos []ports.HistoryViewDTO)
	}

	tests := []testCase{
		{
			name:           "read first page",
			limit:          2,
			offset:         0,
			expectedLen:    2,
			expectedFirst:  "Hello 1",
			expectedSecond: "Response 1",
			expectedNext:   offsets[2],
		},
		{
			name:           "read second page from offset",
			limit:          2,
			offset:         offsets[2],
			expectedLen:    2,
			expectedFirst:  "Hello 2",
			expectedSecond: "Response 2",
		},
		{
			name:   "masking large binary data",
			limit:  1,
			offset: currentOffset,
			setup: func() {
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
			},
			expectedLen: 1,
			validate: func(t *testing.T, dtos []ports.HistoryViewDTO) {
				if !strings.Contains(dtos[0].ContentPreview, "[Attached Image]") {
					t.Errorf("expected masked image, got: %s", dtos[0].ContentPreview)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}
			dtos, nextOffset, err := reader.ReadPage(ctx, tt.limit, tt.offset)
			assertArchivePage(t, dtos, nextOffset, err, tt)
		})
	}
}

func assertArchivePage(t *testing.T, dtos []ports.HistoryViewDTO, nextOffset int64, err error, tt struct {
	name           string
	limit          int
	offset         int64
	setup          func()
	expectedLen    int
	expectedNext   int64
	expectedFirst  string
	expectedSecond string
	validate       func(t *testing.T, dtos []ports.HistoryViewDTO)
}) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: ReadPage() unexpected error: %v", tt.name, err)
	}

	if len(dtos) != tt.expectedLen {
		t.Fatalf("%s: expected %d DTOs, got %d", tt.name, tt.expectedLen, len(dtos))
	}

	if tt.expectedLen >= 1 && tt.expectedFirst != "" {
		if dtos[0].ContentPreview != tt.expectedFirst {
			t.Errorf("%s: unexpected first DTO content: got %q, want %q", tt.name, dtos[0].ContentPreview, tt.expectedFirst)
		}
	}
	if tt.expectedLen >= 2 && tt.expectedSecond != "" {
		if dtos[1].ContentPreview != tt.expectedSecond {
			t.Errorf("%s: unexpected second DTO content: got %q, want %q", tt.name, dtos[1].ContentPreview, tt.expectedSecond)
		}
	}

	if tt.expectedNext != 0 && nextOffset != tt.expectedNext {
		t.Errorf("%s: expected nextOffset %d, got %d", tt.name, tt.expectedNext, nextOffset)
	}

	if tt.validate != nil {
		tt.validate(t, dtos)
	}
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

type readPreviousTestCase struct {
	name           string
	limit          int
	offset         int64
	expectedLen    int
	expectedFirst  string
	expectedSecond string
	expectedNext   int64
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

	tests := []readPreviousTestCase{
		{
			name:           "read last page (limit 2)",
			limit:          2,
			offset:         -1,
			expectedLen:    2,
			expectedFirst:  "Msg 3",
			expectedSecond: "Msg 4",
			expectedNext:   offsets[2],
		},
		{
			name:           "read previous page from offset",
			limit:          2,
			offset:         offsets[2],
			expectedLen:    2,
			expectedFirst:  "Msg 1",
			expectedSecond: "Msg 2",
			expectedNext:   0,
		},
		{
			name:           "read with limit larger than available",
			limit:          10,
			offset:         offsets[2],
			expectedLen:    2,
			expectedFirst:  "Msg 1",
			expectedSecond: "Msg 2",
			expectedNext:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dtos, nextOffset, err := reader.ReadPrevious(ctx, tt.limit, tt.offset)
			assertReadPreviousResult(t, dtos, nextOffset, err, tt)
		})
	}
}

func assertReadPreviousResult(t *testing.T, dtos []ports.HistoryViewDTO, nextOffset int64, err error, tt readPreviousTestCase) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: failed: %v", tt.name, err)
	}
	if len(dtos) != tt.expectedLen {
		t.Fatalf("%s: expected %d, got %d", tt.name, tt.expectedLen, len(dtos))
	}
	if tt.expectedLen >= 1 && dtos[0].ContentPreview != tt.expectedFirst {
		t.Errorf("%s: unexpected first content: got %q, want %q", tt.name, dtos[0].ContentPreview, tt.expectedFirst)
	}
	if tt.expectedLen >= 2 && dtos[1].ContentPreview != tt.expectedSecond {
		t.Errorf("%s: unexpected second content: got %q, want %q", tt.name, dtos[1].ContentPreview, tt.expectedSecond)
	}
	if nextOffset != tt.expectedNext {
		t.Errorf("%s: expected offset %d, got %d", tt.name, tt.expectedNext, nextOffset)
	}
}


func TestJSONLArchiveReader_ReadPrevious_Concurrency(t *testing.T) {
	ctx := context.Background()
	fs := persistence.NewOSFileSystem()
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "archive_concurrency.jsonl")

	line := `{"role":"user","parts":[{"text":"message"}]}` + "\n"
	lineCount := 100
	f, _ := os.Create(archivePath)
	for i := 0; i < lineCount; i++ {
		_, _ = f.Write([]byte(line))
	}
	_ = f.Close()

	reader := history.NewJSONLArchiveReader(fs, archivePath)

	var wg sync.WaitGroup
	numGoroutines := 10
	numCalls := 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numCalls; j++ {
				_, _, err := reader.ReadPrevious(ctx, 5, -1)
				if err != nil {
					t.Errorf("ReadPrevious failed: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}
