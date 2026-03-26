// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package suggestions

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type mockPromptTracker struct {
	mu      sync.RWMutex
	prompts []string
}

func (m *mockPromptTracker) Append(prompt string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prompts = append(m.prompts, prompt)
	return nil
}

func (m *mockPromptTracker) LoadTopN(ctx context.Context, limit int) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if limit >= len(m.prompts) {
		return m.prompts, nil
	}
	return m.prompts[:limit], nil
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

	service, err := NewMultiSourceSuggestionService(tracker, []string{"hello", "world"})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	tests := []struct {
		name     string
		prefix   string
		expected []string
	}{
		{
			name:     "trie search",
			prefix:   "tes",
			expected: []string{"test-prompt-1", "test-prompt-2"},
		},
		{
			name:     "empty prefix",
			prefix:   "",
			expected: []string{"hello", "test-prompt-1", "test-prompt-2", "world"}, // Sorted trie results
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
	service, _ := NewMultiSourceSuggestionService(tracker, nil)

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
	service, _ := NewMultiSourceSuggestionService(tracker, nil)

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
	tracker := &mockPromptTracker{}
	service, _ := NewMultiSourceSuggestionService(tracker, nil)

	prompt := "new-unique-prompt"
	err := service.RecordPrompt(prompt)
	if err != nil {
		t.Fatalf("RecordPrompt failed: %v", err)
	}

	// Check immediate trie update
	got, _ := service.GetSuggestions(context.Background(), "new")
	if len(got) != 1 || got[0] != prompt {
		t.Errorf("prompt not immediately available in trie: %v", got)
	}

	// Wait for goroutine to finish (short sleep is okay in test here)
	time.Sleep(100 * time.Millisecond)

	// Check persistence in tracker
	prompts := tracker.GetPrompts()
	if len(prompts) != 1 || prompts[0] != prompt {
		t.Errorf("prompt not persisted in tracker: %v", prompts)
	}
}

func TestMultiSourceSuggestionService_MergeSuggestions(t *testing.T) {
	s := &MultiSourceSuggestionService{}
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
