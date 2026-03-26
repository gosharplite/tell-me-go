// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package suggestions

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/pkg/trie"
)

var _ ports.SuggestionService = (*MultiSourceSuggestionService)(nil)

// MultiSourceSuggestionService aggregates suggestions from various sources.
type MultiSourceSuggestionService struct {
	trie    *trie.Trie
	tracker ports.PromptTracker
}

// NewMultiSourceSuggestionService creates a new suggestion service and pre-loads the trie.
func NewMultiSourceSuggestionService(tracker ports.PromptTracker, toolNames []string, recentHistory []string) (*MultiSourceSuggestionService, error) {
	s := &MultiSourceSuggestionService{
		trie:    trie.NewTrie(),
		tracker: tracker,
	}

	// 1. Pre-load Global Top Prompts
	topPrompts, err := tracker.LoadTopN(context.Background(), 50)
	if err != nil {
		// Log error but continue with what we have
		fmt.Fprintf(os.Stderr, "Warning: failed to load top prompts: %v\n", err)
	}
	for _, p := range topPrompts {
		s.trie.Insert(p)
	}

	// 2. Pre-load Active Session History
	for _, h := range recentHistory {
		s.trie.Insert(h)
	}

	// 3. Pre-load Tool Names
	for _, t := range toolNames {
		s.trie.Insert(t)
	}

	return s, nil
}

// GetSuggestions returns up to 10 suggestions based on the prefix.
func (s *MultiSourceSuggestionService) GetSuggestions(ctx context.Context, prefix string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// If prefix is empty, we can return some default top prompts
	if prefix == "" {
		return s.trie.SearchPrefix("", 5), nil
	}

	// 1. Trie Search
	suggestions := s.trie.SearchPrefix(prefix, 10)

	// 2. File System Search if it looks like a path
	if strings.Contains(prefix, string(os.PathSeparator)) || strings.Contains(prefix, "/") || strings.HasPrefix(prefix, ".") {
		fileSuggestions := s.scanFiles(ctx, prefix)
		suggestions = s.mergeSuggestions(suggestions, fileSuggestions, 10)
	}

	return suggestions, nil
}

// RecordPrompt records a user prompt into both the Trie and the global tracker.
func (s *MultiSourceSuggestionService) RecordPrompt(prompt string) error {
	if prompt == "" {
		return nil
	}

	// 1. Immediate in-memory update
	s.trie.Insert(prompt)

	// 2. Fire-and-forget persistent update
	go func() {
		_ = s.tracker.Append(prompt)
	}()

	return nil
}

func (s *MultiSourceSuggestionService) scanFiles(ctx context.Context, prefix string) []string {
	dir, filePrefix := filepath.Split(prefix)
	if dir == "" {
		dir = "."
	}

	f, err := os.Open(dir)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	var results []string
	// Read directory in small batches to allow context cancellation
	batchSize := 100

	for {
		// Check cancellation before each batch
		if ctx.Err() != nil {
			return results
		}

		entries, err := f.ReadDir(batchSize)
		if err != nil {
			// io.EOF or other errors
			break
		}

		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, filePrefix) {
				if name == ".git" || name == "node_modules" || name == "vendor" || name == "bin" || name == "obj" {
					continue
				}

				fullPath := filepath.Join(dir, name)
				if entry.IsDir() {
					fullPath += string(os.PathSeparator)
				}
				results = append(results, fullPath)
				if len(results) >= 10 {
					return results
				}
			}
		}
	}

	return results
}

func (s *MultiSourceSuggestionService) mergeSuggestions(s1, s2 []string, limit int) []string {
	seen := make(map[string]bool)
	var merged []string

	for _, s := range s1 {
		if !seen[s] {
			seen[s] = true
			merged = append(merged, s)
		}
	}

	for _, s := range s2 {
		if len(merged) >= limit {
			break
		}
		if !seen[s] {
			seen[s] = true
			merged = append(merged, s)
		}
	}

	if len(merged) > limit {
		return merged[:limit]
	}
	return merged
}
