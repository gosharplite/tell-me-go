// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domainpersistence "github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// appendContent writes a single llm.Content as a JSON line to the archive
// and returns the byte offset where the line was written.
func appendContent(archivePath string, c *llm.Content) int64 {
	stat, err := os.Stat(archivePath)
	var start int64
	if err == nil && stat != nil {
		start = stat.Size()
	}
	f, err := os.OpenFile(archivePath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		panic(fmt.Sprintf("appendContent: %v", err))
	}
	defer func() { _ = f.Close() }()
	data, _ := json.Marshal(c)
	data = append(data, '\n')
	_, _ = f.Write(data)
	return start
}

func validateFirstPage(t *testing.T, dtos []ports.HistoryViewDTO, nextOffset int64, offsets []int64) {
	t.Helper()
	if len(dtos) != 2 || dtos[0].ContentPreview != "Hello 1" {
		t.Errorf("got %d dtos, first: %q", len(dtos), dtos[0].ContentPreview)
	}
	if nextOffset != offsets[2] {
		t.Errorf("expected nextOffset %d, got %d", offsets[2], nextOffset)
	}
}

func validateSecondPage(t *testing.T, dtos []ports.HistoryViewDTO, nextOffset int64, offsets []int64) {
	t.Helper()
	if len(dtos) != 2 || dtos[0].ContentPreview != "Hello 2" {
		t.Errorf("got %d dtos, first: %q", len(dtos), dtos[0].ContentPreview)
	}
}

func validateMaskingLargeBinaryData(t *testing.T, dtos []ports.HistoryViewDTO, nextOffset int64, offsets []int64) {
	t.Helper()
	if len(dtos) != 1 {
		t.Fatalf("expected 1 dto, got %d", len(dtos))
	}
	if !strings.Contains(dtos[0].ContentPreview, "[Attached Image]") {
		t.Errorf("expected masked image, got: %s", dtos[0].ContentPreview)
	}
}

func validateComplexContentParts(t *testing.T, dtos []ports.HistoryViewDTO, nextOffset int64, offsets []int64) {
	t.Helper()
	if len(dtos) != 1 {
		t.Fatalf("expected 1 dto, got %d", len(dtos))
	}
	if dtos[0].ThoughtProcess != "Thinking..." {
		t.Errorf("expected thought 'Thinking...', got %q", dtos[0].ThoughtProcess)
	}
	if !strings.Contains(dtos[0].ContentPreview, "Result:") {
		t.Errorf("expected preview to contain 'Result:', got %q", dtos[0].ContentPreview)
	}
	if !strings.Contains(dtos[0].ContentPreview, "[Attached Asset]") {
		t.Errorf("expected preview to contain '[Attached Asset]', got %q", dtos[0].ContentPreview)
	}
	if len(dtos[0].ToolCalls) != 1 || dtos[0].ToolCalls[0] != "get_weather" {
		t.Errorf("expected tool call 'get_weather', got %v", dtos[0].ToolCalls)
	}
}

func validateFunctionResponsePart(t *testing.T, dtos []ports.HistoryViewDTO, nextOffset int64, offsets []int64) {
	t.Helper()
	if len(dtos) != 1 {
		t.Fatalf("expected 1 dto, got %d", len(dtos))
	}
	if len(dtos[0].ToolCalls) != 1 || dtos[0].ToolCalls[0] != "get_weather" {
		t.Errorf("expected tool call 'get_weather', got %v", dtos[0].ToolCalls)
	}
}

func TestJSONLArchiveReader_ReadPage(t *testing.T) {
	ctx := context.Background()
	fs := persistence.NewOSFileSystem()

	tests := []struct {
		name       string
		contents   []*llm.Content
		limit      int
		startIndex int // The index in 'contents' to use as the starting offset
		validate   func(t *testing.T, dtos []ports.HistoryViewDTO, nextOffset int64, offsets []int64)
	}{
		{
			name: "read first page",
			contents: []*llm.Content{
				{Role: "user", Parts: []*llm.Part{{Text: "Hello 1"}}},
				{Role: "assistant", Parts: []*llm.Part{{Text: "Response 1"}}},
				{Role: "user", Parts: []*llm.Part{{Text: "Hello 2"}}},
				{Role: "assistant", Parts: []*llm.Part{{Text: "Response 2"}}},
			},
			limit:      2,
			startIndex: 0,
			validate:   validateFirstPage,
		},
		{
			name: "read second page",
			contents: []*llm.Content{
				{Role: "user", Parts: []*llm.Part{{Text: "Hello 1"}}},
				{Role: "assistant", Parts: []*llm.Part{{Text: "Response 1"}}},
				{Role: "user", Parts: []*llm.Part{{Text: "Hello 2"}}},
				{Role: "assistant", Parts: []*llm.Part{{Text: "Response 2"}}},
			},
			limit:      2,
			startIndex: 2,
			validate:   validateSecondPage,
		},
		{
			name: "masking large binary data",
			contents: []*llm.Content{
				{
					Role: "user",
					Parts: []*llm.Part{
						{Text: "A picture:"},
						{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("fake binary data")}},
					},
				},
			},
			limit:      1,
			startIndex: 0,
			validate:   validateMaskingLargeBinaryData,
		},
		{
			name: "complex content parts",
			contents: []*llm.Content{
				{
					Role: "assistant",
					Parts: []*llm.Part{
						{IsThought: true, Text: "Thinking..."},
						{Text: "Result:"},
						{FunctionCall: &llm.FunctionCall{Name: "get_weather"}},
						{AssetID: "asset-123"},
						nil,
					},
				},
			},
			limit:      1,
			startIndex: 0,
			validate:   validateComplexContentParts,
		},
		{
			name: "function response part",
			contents: []*llm.Content{
				{
					Role: "user",
					Parts: []*llm.Part{
						{FunctionResponse: &llm.FunctionResponse{Name: "get_weather", Response: map[string]interface{}{"temp": 22}}},
					},
				},
			},
			limit:      1,
			startIndex: 0,
			validate:   validateFunctionResponsePart,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			archivePath := filepath.Join(tmpDir, "archive.jsonl")

			var offsets []int64
			for _, c := range tt.contents {
				off := appendContent(archivePath, c)
				offsets = append(offsets, off)
			}

			reader := history.NewJSONLArchiveReader(fs, archivePath)
			startOffset := int64(0)
			if len(offsets) > tt.startIndex {
				startOffset = offsets[tt.startIndex]
			}

			dtos, nextOffset, err := reader.ReadPage(ctx, tt.limit, startOffset)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			tt.validate(t, dtos, nextOffset, offsets)
		})
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

func TestJSONLArchiveReader_Resilience(t *testing.T) {
	ctx := context.Background()
	fs := persistence.NewOSFileSystem()
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "resilience.jsonl")

	valid1 := `{"role":"user","parts":[{"text":"valid 1"}]}` + "\n"
	invalid := `not json at all` + "\n"
	partial := `{"role":"assistant","parts":[{"text":"partial` + "\n"
	emptyLines := "\n\n\n"
	largeLineText := strings.Repeat("A", 10000)
	largeLine := `{"role":"user","parts":[{"text":"` + largeLineText + `"}]}` + "\n"
	valid2 := `{"role":"assistant","parts":[{"text":"valid 2"}]}` + "\n"

	content := valid1 + invalid + partial + emptyLines + largeLine + valid2
	err := os.WriteFile(archivePath, []byte(content), 0644)
	require.NoError(t, err, "failed to write resilience test file")

	reader := history.NewJSONLArchiveReader(fs, archivePath)

	t.Run("ReadPage handles corruption", func(t *testing.T) {
		// Read 10 entries (should get 3 valid ones: valid1, largeLine, valid2)
		dtos, nextOffset, err := reader.ReadPage(ctx, 10, 0)
		require.NoError(t, err, "ReadPage failed")

		require.Len(t, dtos, 3, "expected 3 valid DTOs")

		assert.Equal(t, "valid 1", dtos[0].ContentPreview)
		assert.Equal(t, largeLineText, dtos[1].ContentPreview)
		assert.Equal(t, "valid 2", dtos[2].ContentPreview)

		assert.Equal(t, int64(len(content)), nextOffset)
	})

	t.Run("ReadPrevious handles corruption", func(t *testing.T) {
		// Read 10 entries backwards from EOF
		dtos, startOffset, err := reader.ReadPrevious(ctx, 10, -1)
		require.NoError(t, err, "ReadPrevious failed")

		require.Len(t, dtos, 3, "expected 3 valid DTOs")

		assert.Equal(t, "valid 1", dtos[0].ContentPreview)
		assert.Equal(t, largeLineText, dtos[1].ContentPreview)
		assert.Equal(t, "valid 2", dtos[2].ContentPreview)

		assert.Equal(t, int64(0), startOffset)
	})
}

func TestJSONLArchiveReader_Errors(t *testing.T) {
	ctx := context.Background()
	fs := persistence.NewOSFileSystem()
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "is_a_dir")
	_ = os.Mkdir(archivePath, 0755)

	reader := history.NewJSONLArchiveReader(fs, archivePath)

	t.Run("ReadPage error on directory", func(t *testing.T) {
		_, _, err := reader.ReadPage(ctx, 10, 0)
		if err == nil {
			t.Error("expected error when reading a directory as an archive file")
		}
	})

	t.Run("ReadPrevious error on indexing a directory", func(t *testing.T) {
		_, _, err := reader.ReadPrevious(ctx, 10, -1)
		if err == nil {
			t.Error("expected error during indexing a directory")
		}
	})
}

func TestJSONLArchiveReader_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	fs := persistence.NewOSFileSystem()
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "archive.jsonl")
	_ = os.WriteFile(archivePath, []byte(`{"role":"user","parts":[{"text":"message"}]}`+"\n"), 0644)

	reader := history.NewJSONLArchiveReader(fs, archivePath)

	_, _, err := reader.ReadPage(ctx, 10, 0)
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("expected context canceled error, got: %v", err)
	}
}

func TestJSONLArchiveReader_ReadPrevious_EdgeCases(t *testing.T) {
	ctx := context.Background()
	fs := persistence.NewOSFileSystem()
	tmpDir := t.TempDir()

	t.Run("empty archive", func(t *testing.T) {
		archivePath := filepath.Join(tmpDir, "empty.jsonl")
		_ = os.WriteFile(archivePath, []byte(""), 0644)
		reader := history.NewJSONLArchiveReader(fs, archivePath)
		dtos, startOffset, err := reader.ReadPrevious(ctx, 10, -1)
		if err != nil {
			t.Fatal(err)
		}
		if len(dtos) != 0 {
			t.Errorf("expected 0 dtos, got %d", len(dtos))
		}
		if startOffset != 0 {
			t.Errorf("expected startOffset 0, got %d", startOffset)
		}
	})

	t.Run("offset at zero", func(t *testing.T) {
		archivePath := filepath.Join(tmpDir, "one.jsonl")
		_ = os.WriteFile(archivePath, []byte(`{"role":"user","parts":[{"text":"hi"}]}`+"\n"), 0644)
		reader := history.NewJSONLArchiveReader(fs, archivePath)
		dtos, startOffset, err := reader.ReadPrevious(ctx, 10, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(dtos) != 0 {
			t.Errorf("expected 0 dtos, got %d", len(dtos))
		}
		if startOffset != 0 {
			t.Errorf("expected startOffset 0, got %d", startOffset)
		}
	})
}

func TestJSONLArchiveReader_LargeLines(t *testing.T) {
	ctx := context.Background()
	fs := persistence.NewOSFileSystem()
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "large_lines.jsonl")

	// Create a line significantly larger than bufio's 4KB default buffer
	largeText := strings.Repeat("A", 10000)
	largeLine := `{"role":"user","parts":[{"text":"` + largeText + `"}]}` + "\n"
	normalLine := `{"role":"assistant","parts":[{"text":"short"}]}` + "\n"

	content := largeLine + normalLine
	if err := os.WriteFile(archivePath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write large lines file: %v", err)
	}

	reader := history.NewJSONLArchiveReader(fs, archivePath)

	// ReadPrevious triggers buildIndex
	dtos, startOffset, err := reader.ReadPrevious(ctx, 10, -1)
	if err != nil {
		t.Fatalf("ReadPrevious failed: %v", err)
	}

	if len(dtos) != 2 {
		t.Fatalf("expected 2 DTOs, got %d", len(dtos))
	}
	if dtos[0].ContentPreview != largeText {
		t.Errorf("expected large text, got length %d", len(dtos[0].ContentPreview))
	}
	if dtos[1].ContentPreview != "short" {
		t.Errorf("expected 'short', got %q", dtos[1].ContentPreview)
	}
	if startOffset != 0 {
		t.Errorf("expected startOffset 0, got %d", startOffset)
	}
}

type errorFileSystem struct {
	domainpersistence.FileSystem
	openErr error
}

func (e *errorFileSystem) Open(ctx context.Context, name string) (domainpersistence.File, error) {
	return nil, e.openErr
}

func TestJSONLArchiveReader_IndexingReadError(t *testing.T) {
	ctx := context.Background()
	fs := &errorFileSystem{
		FileSystem: persistence.NewOSFileSystem(),
		openErr:    fmt.Errorf("injected open error"),
	}
	archivePath := "some_path.jsonl"

	reader := history.NewJSONLArchiveReader(fs, archivePath)

	// ReadPrevious triggers buildIndex which should fail
	_, _, err := reader.ReadPrevious(ctx, 10, -1)
	if err == nil || !strings.Contains(err.Error(), "injected open error") {
		t.Errorf("expected injected open error, got %v", err)
	}
}

func TestJSONLArchiveReader_ReadPrevious_NonExistent(t *testing.T) {
	ctx := context.Background()
	fs := persistence.NewOSFileSystem()
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "does_not_exist.jsonl")

	reader := history.NewJSONLArchiveReader(fs, archivePath)
	dtos, startOffset, err := reader.ReadPrevious(ctx, 10, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(dtos) != 0 {
		t.Errorf("expected 0 dtos, got %d", len(dtos))
	}
	if startOffset != 0 {
		t.Errorf("expected startOffset 0, got %d", startOffset)
	}
}

type mockFile struct {
	domainpersistence.File
	name       string
	data       *bytes.Buffer
	readFunc   func(p []byte) (n int, err error)
	readAtFunc func(p []byte, off int64) (n int, err error)
}

func (f *mockFile) Read(p []byte) (n int, err error) {
	if f.readFunc != nil {
		return f.readFunc(p)
	}
	return f.data.Read(p)
}

func (f *mockFile) ReadAt(p []byte, off int64) (n int, err error) {
	if f.readAtFunc != nil {
		return f.readAtFunc(p, off)
	}
	return 0, fmt.Errorf("ReadAt not implemented in mock")
}

func (f *mockFile) Close() error {
	return nil
}

func (f *mockFile) Name() string {
	return f.name
}

type mockFileSystem struct {
	domainpersistence.FileSystem
	openFunc func(ctx context.Context, name string) (domainpersistence.File, error)
}

func (m *mockFileSystem) Open(ctx context.Context, name string) (domainpersistence.File, error) {
	if m.openFunc != nil {
		return m.openFunc(ctx, name)
	}
	return nil, os.ErrNotExist
}

func TestJSONLArchiveReader_ReadPage_ReadError(t *testing.T) {
	ctx := context.Background()
	archivePath := "read_error.jsonl"

	// Mock file that fails on ReadAt (used by SectionReader in readPageInternal)
	errorFile := &mockFile{
		name: archivePath,
		readAtFunc: func(p []byte, off int64) (n int, err error) {
			return 0, fmt.Errorf("injected read error")
		},
	}

	fs := &mockFileSystem{
		openFunc: func(ctx context.Context, name string) (domainpersistence.File, error) {
			return errorFile, nil
		},
	}

	reader := history.NewJSONLArchiveReader(fs, archivePath)
	_, _, err := reader.ReadPage(ctx, 10, 0)
	if err == nil || !strings.Contains(err.Error(), "injected read error") {
		t.Errorf("expected injected read error, got %v", err)
	}
}
