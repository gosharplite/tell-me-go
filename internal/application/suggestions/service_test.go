// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package suggestions

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

type mockPromptTracker struct {
	mu      sync.RWMutex
	prompts []string
	appended chan struct{}
}

func (m *mockPromptTracker) Append(ctx context.Context, prompt string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prompts = append(m.prompts, prompt)
	if m.appended != nil {
		select {
		case m.appended <- struct{}{}:
		default:
		}
	}
	return nil
}

func (m *mockPromptTracker) LoadTopN(ctx context.Context, limit int) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := len(m.prompts)
	if limit < n {
		n = limit
	}
	res := make([]string, n)
	copy(res, m.prompts[:n])
	return res, nil
}

func (m *mockPromptTracker) GetPrompts() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.prompts
}

func TestMultiSourceSuggestionService_GetSuggestions(t *testing.T) {
	tracker := &mockPromptTracker{
		prompts: []string{"test-prompt-1", "test-prompt-2"},
	}

	service, err := NewMultiSourceSuggestionService(infra_persistence.NewOSFileSystem(), tracker, []string{"hello", "world"}, io.Discard)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	tests := []struct {
		name     string
		prefix   string
		expected []string
	}{
		{
			name:     "trie search (prefix match still works)",
			prefix:   "tes",
			expected: []string{"test-prompt-1", "test-prompt-2"},
		},
		{
			name:     "fuzzy search - subsequence",
			prefix:   "tp1",
			expected: []string{"test-prompt-1"},
		},
		{
			name:     "empty prefix returns first 5 insertion-ordered items",
			prefix:   "",
			expected: []string{"test-prompt-1", "test-prompt-2", "hello", "world"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := service.GetSuggestions(context.Background(), tt.prefix)
			if err != nil {
				t.Fatalf("GetSuggestions failed: %v", err)
			}
			if len(got) != len(tt.expected) {
				t.Errorf("got %d suggestions; want %d. Got: %v", len(got), len(tt.expected), got)
			}
			for i, v := range got {
				if v != tt.expected[i] {
					t.Errorf("at index %d: got %q; want %q", i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestMultiSourceSuggestionService_ContextCancellation(t *testing.T) {
	tracker := &mockPromptTracker{}
	service, _ := NewMultiSourceSuggestionService(infra_persistence.NewOSFileSystem(), tracker, nil, io.Discard)

	// Create many files to make scan slow
	tmpDir := t.TempDir()
	for i := 0; i < 1000; i++ {
		_ = os.WriteFile(filepath.Join(tmpDir, "test-file-"+string(rune(i))), []byte(""), 0644)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := service.GetSuggestions(ctx, tmpDir+string(os.PathSeparator))
	if err == nil {
		t.Error("expected context canceled error, got nil")
	}
}

func TestMultiSourceSuggestionService_FileSystemSearch(t *testing.T) {
	tracker := &mockPromptTracker{}
	service, _ := NewMultiSourceSuggestionService(infra_persistence.NewOSFileSystem(), tracker, nil, io.Discard)

	// Create some test files
	tmpDir := t.TempDir()
	files := []string{"foo.txt", "bar.txt", "baz.txt", ".git"}
	for _, f := range files {
		_ = os.WriteFile(filepath.Join(tmpDir, f), []byte(""), 0644)
	}

	prefix := filepath.Join(tmpDir, "ba")
	got, err := service.GetSuggestions(context.Background(), prefix)
	if err != nil {
		t.Fatalf("GetSuggestions failed: %v", err)
	}

	// Should match bar.txt and baz.txt
	expected := []string{
		filepath.Join(tmpDir, "bar.txt"),
		filepath.Join(tmpDir, "baz.txt"),
	}
	if len(got) != 2 {
		t.Errorf("got %d suggestions; want 2. Got: %v", len(got), got)
	}
	// Check content
	for _, v := range got {
		found := false
		for _, e := range expected {
			if v == e {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("unexpected suggestion: %q", v)
		}
	}
}

func TestMultiSourceSuggestionService_RecordPrompt(t *testing.T) {
	appended := make(chan struct{}, 1)
	tracker := &mockPromptTracker{
		appended: appended,
	}
	service, _ := NewMultiSourceSuggestionService(infra_persistence.NewOSFileSystem(), tracker, nil, io.Discard)

	prompt := "new-unique-prompt"
	err := service.RecordPrompt(context.Background(), prompt)
	if err != nil {
		t.Fatalf("RecordPrompt failed: %v", err)
	}

	// Check immediate trie update
	got, _ := service.GetSuggestions(context.Background(), "new")
	if len(got) != 1 || got[0] != prompt {
		t.Errorf("prompt not immediately available in trie: %v", got)
	}

	// Wait for persistence in tracker (async)
	select {
	case <-appended:
		// success
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for prompt persistence")
	}

	// Check persistence in tracker
	prompts := tracker.GetPrompts()
	if len(prompts) != 1 || prompts[0] != prompt {
		t.Errorf("prompt not persisted in tracker: %v", prompts)
	}
}

func TestMultiSourceSuggestionService_MergeSuggestions(t *testing.T) {
	s := &multiSourceSuggestionService{logger: io.Discard}
	tests := []struct {
		name     string
		s1       []string
		s2       []string
		limit    int
		expected []string
	}{
		{
			name:     "duplicates across slices",
			s1:       []string{"a", "b"},
			s2:       []string{"b", "c"},
			limit:    5,
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "limit exactly reached during s1",
			s1:       []string{"a", "b"},
			s2:       []string{"c"},
			limit:    2,
			expected: []string{"a", "b"},
		},
		{
			name:     "limit strictly reached during s2",
			s1:       []string{"a"},
			s2:       []string{"b", "c", "d"},
			limit:    3,
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "empty slices",
			s1:       []string{},
			s2:       []string{},
			limit:    5,
			expected: nil,
		},
		{
			name:     "s1 larger than limit",
			s1:       []string{"a", "b", "c"},
			s2:       []string{"d"},
			limit:    2,
			expected: []string{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.mergeSuggestions(tt.s1, tt.s2, tt.limit)
			if len(got) != len(tt.expected) {
				t.Errorf("got length %d; want %d", len(got), len(tt.expected))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("at index %d: got %q; want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestSuggestionService_ScanFiles_CancelledContext(t *testing.T) {
	tracker := &mockPromptTracker{}
	service, _ := NewMultiSourceSuggestionService(infra_persistence.NewOSFileSystem(), tracker, nil, io.Discard)

	// Create a temp directory with some files
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "test1.txt"), []byte(""), 0644)

	// Case 1: Pre-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Calling scanFiles indirectly via GetSuggestions
	// Need to ensure the prefix looks like a path
	prefix := tmpDir + string(os.PathSeparator) + "t"

	// We call GetSuggestions. It checks ctx.Err() at the beginning.
	// But we also want to hit the check inside scanFiles loop.
	// scanFiles is called AFTER the initial check in GetSuggestions.

	results, err := service.GetSuggestions(ctx, prefix)
	if err == nil {
		t.Errorf("expected error from GetSuggestions with cancelled context, got nil")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}

	// To specifically cover the check INSIDE scanFiles (line 108-110),
	// we'd ideally want a context that cancels MID-SCAN.
	// But since GetSuggestions checks it first, we might need to call scanFiles directly if possible,
	// or use a context that cancels after a short delay if the scan is slow enough.
	// Since scanFiles is unexported, we can use the receiver.

	ctx2, cancel2 := context.WithCancel(context.Background())
	// We can't easily trigger cancellation mid-scan without a very large directory or a hook.
	// However, if we call GetSuggestions with a NON-cancelled context, it passes the first check.
	// If we then cancel it, and scanFiles is slow...
	// Let's just call scanFiles via the service struct since we are in the same package.

	cancel2()
	res := service.(*multiSourceSuggestionService).scanFiles(ctx2, prefix)
	if len(res) != 0 {
		t.Errorf("scanFiles: expected 0 results for cancelled context, got %d", len(res))
	}
}

func TestSuggestionService_ScanFiles_InvalidDir(t *testing.T) {
	tracker := &mockPromptTracker{}
	service, _ := NewMultiSourceSuggestionService(infra_persistence.NewOSFileSystem(), tracker, nil, io.Discard)

	// Call GetSuggestions with a path that definitely doesn't exist
	invalidPath := filepath.Join(t.TempDir(), "non-existent-dir", "file")

	// Ensure it looks like a path to trigger scanFiles
	prefix := invalidPath

	got, err := service.GetSuggestions(context.Background(), prefix)
	if err != nil {
		t.Fatalf("GetSuggestions failed: %v", err)
	}

	// Should return nil/empty results from scanFiles and whatever is in trie
	if len(got) != 0 {
		t.Errorf("expected 0 suggestions for invalid path, got %v", got)
	}
}

type errorPromptTracker struct {
	mockPromptTracker
}

func (e *errorPromptTracker) LoadTopN(ctx context.Context, limit int) ([]string, error) {
	return nil, fmt.Errorf("forced error")
}

func TestSuggestionService_LoadTopNFails(t *testing.T) {
	tracker := &errorPromptTracker{}
	// This should log to stderr but not return an error
	service, err := NewMultiSourceSuggestionService(infra_persistence.NewOSFileSystem(), tracker, nil, io.Discard)
	if err != nil {
		t.Fatalf("NewMultiSourceSuggestionService should not fail when tracker fails to load: %v", err)
	}
	if service == nil {
		t.Fatal("service is nil")
	}
}

func TestSuggestionService_RecordPrompt_EmptyPath(t *testing.T) {
	tracker := &mockPromptTracker{}
	service, _ := NewMultiSourceSuggestionService(infra_persistence.NewOSFileSystem(), tracker, nil, io.Discard)

	err := service.RecordPrompt(context.Background(), "")
	if err != nil {
		t.Errorf("RecordPrompt(\"\") should return nil, got %v", err)
	}

	// Verify tracker wasn't called
	if len(tracker.GetPrompts()) != 0 {
		t.Errorf("expected 0 prompts in tracker, got %d", len(tracker.GetPrompts()))
	}
}

func TestMultiSourceSuggestionService_ScanFiles_ExclusionsAndLimit(t *testing.T) {
	tracker := &mockPromptTracker{}
	service, _ := NewMultiSourceSuggestionService(infra_persistence.NewOSFileSystem(), tracker, nil, io.Discard)

	tmpDir := t.TempDir()

	// 1. Check directory path separator (Line 127)
	_ = os.MkdirAll(filepath.Join(tmpDir, "sub-dir"), 0755)
	prefix := tmpDir + string(os.PathSeparator) + "s"
	got, _ := service.GetSuggestions(context.Background(), prefix)
	foundDir := false
	for _, v := range got {
		if strings.HasSuffix(v, string(os.PathSeparator)) {
			foundDir = true
			break
		}
	}
	if !foundDir {
		t.Errorf("expected directory to have path separator, got: %v", got)
	}

	// 2. Check exclusions (Line 122)
	_ = os.MkdirAll(filepath.Join(tmpDir, "vendor"), 0755)
	_ = os.MkdirAll(filepath.Join(tmpDir, "node_modules"), 0755)
	prefix2 := tmpDir + string(os.PathSeparator)
	got2, _ := service.GetSuggestions(context.Background(), prefix2)
	for _, v := range got2 {
		if strings.Contains(v, "vendor") || strings.Contains(v, "node_modules") {
			t.Errorf("found excluded directory in suggestions: %q", v)
		}
	}

	// 3. Check limit (Line 131)
	for i := 0; i < 20; i++ {
		_ = os.WriteFile(filepath.Join(tmpDir, fmt.Sprintf("file-%d.txt", i)), []byte(""), 0644)
	}
	prefix3 := tmpDir + string(os.PathSeparator) + "f"
	got3, _ := service.GetSuggestions(context.Background(), prefix3)
	if len(got3) != 10 {
		t.Errorf("expected 10 suggestions, got %d", len(got3))
	}
}

type chunkedMockFile struct {
	persistence.File
	ctx        context.Context
	cancel     context.CancelFunc
	readCalled int32
}

func (f *chunkedMockFile) ReadDir(n int) ([]os.DirEntry, error) {
	called := atomic.AddInt32(&f.readCalled, 1)

	// Trigger cancellation during the first call
	if called == 1 {
		f.cancel()
	}

	res := make([]os.DirEntry, n)
	for i := 0; i < n; i++ {
		res[i] = &chunkedMockDirEntry{name: "matching-file", isDir: false}
	}

	return res, nil
}

func (f *chunkedMockFile) Close() error {
	return nil
}

// Dummy implementations for required methods of persistence.File
func (f *chunkedMockFile) Read(p []byte) (n int, err error)             { return 0, io.EOF }
func (f *chunkedMockFile) Write(p []byte) (n int, err error)            { return 0, nil }
func (f *chunkedMockFile) Seek(offset int64, whence int) (int64, error) { return 0, nil }

type chunkedMockDirEntry struct {
	name  string
	isDir bool
}

func (m *chunkedMockDirEntry) Name() string               { return m.name }
func (m *chunkedMockDirEntry) IsDir() bool                { return m.isDir }
func (m *chunkedMockDirEntry) Type() os.FileMode          { return 0 }
func (m *chunkedMockDirEntry) Info() (os.FileInfo, error) { return nil, nil }

type controlledMockFS struct {
	persistence.FileSystem
	file persistence.File
}

func (m *controlledMockFS) Open(ctx context.Context, name string) (persistence.File, error) {
	return m.file, nil
}

func TestScanFiles_RespectsCancellationBetweenChunks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockFile := &chunkedMockFile{
		ctx:    ctx,
		cancel: cancel,
	}

	fs := &controlledMockFS{
		file: mockFile,
	}

	service := &multiSourceSuggestionService{
		fs:     fs,
		logger: io.Discard,
	}

	// Calling scanFiles.
	// 1st iteration:
	//   ctx.Err() is nil
	//   ReadDir(100) is called. It returns 100 entries AND calls cancel().
	// 2nd iteration:
	//   ctx.Err() is now non-nil (context cancelled)
	//   The loop should exit early BEFORE calling ReadDir(100) again.

	service.scanFiles(ctx, "mat")

	calledCount := atomic.LoadInt32(&mockFile.readCalled)
	if calledCount != 1 {
		t.Errorf("expected ReadDir to be called exactly once, but it was called %d times", calledCount)
	}
}

func assertSuggestionsMatch(t *testing.T, got, expected []string) {
	t.Helper()
	if len(got) != len(expected) {
		t.Fatalf("expected %d suggestions, got %d: %v", len(expected), len(got), got)
	}
	for i, v := range got {
		if v != expected[i] {
			t.Errorf("at index %d: got %q; want %q", i, v, expected[i])
		}
	}
}

func TestMultiSourceSuggestionService_RecordPrompt_MRU(t *testing.T) {
	tracker := &mockPromptTracker{}
	service, _ := NewMultiSourceSuggestionService(infra_persistence.NewOSFileSystem(), tracker, nil, io.Discard)

	// 1. Record multiple prompts
	prompts := []string{"p1", "p2", "p3"}
	for _, p := range prompts {
		_ = service.RecordPrompt(context.Background(), p)
	}

	// 2. Check MRU order (newest first)
	got, _ := service.GetSuggestions(context.Background(), "")
	expected := []string{"p3", "p2", "p1"}
	assertSuggestionsMatch(t, got, expected)

	// 3. Re-record an existing prompt (should move to top)
	_ = service.RecordPrompt(context.Background(), "p1")
	got, _ = service.GetSuggestions(context.Background(), "")
	expected = []string{"p1", "p3", "p2"}
	assertSuggestionsMatch(t, got, expected)

	// 4. Test bounded capacity
	for i := 0; i < 150; i++ {
		_ = service.RecordPrompt(context.Background(), fmt.Sprintf("bulk-%d", i))
	}
	got, _ = service.GetSuggestions(context.Background(), "")
	// Should only have 5 (UI limit) but the underlying history should be limited to 100
	if len(got) != 5 {
		t.Errorf("expected 5 suggestions (UI limit), got %d", len(got))
	}

	// Access the underlying history to check the 100 limit
	history := service.(*multiSourceSuggestionService).history
	if len(history) != 100 {
		t.Errorf("expected history to be limited to 100, got %d", len(history))
	}
	if history[0] != "bulk-149" {
		t.Errorf("expected newest item at index 0, got %q", history[0])
	}
}

func TestMultiSourceSuggestionService_Close_WaitsForBackgroundTasks(t *testing.T) {
	appended := make(chan struct{})
	tracker := &mockPromptTracker{
		appended: appended,
	}
	service, _ := NewMultiSourceSuggestionService(infra_persistence.NewOSFileSystem(), tracker, nil, io.Discard)

	// Record a prompt
	prompt := "close-test-prompt"
	_ = service.RecordPrompt(context.Background(), prompt)

	// Immediately call Close. It should wait for the background goroutine to finish.
	err := service.Close(context.Background())
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify it was persisted
	prompts := tracker.GetPrompts()
	if len(prompts) != 1 || prompts[0] != prompt {
		t.Errorf("prompt not persisted after Close: %v", prompts)
	}
}
