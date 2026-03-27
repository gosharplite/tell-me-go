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

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/pkg/matcher"
)

var _ ports.SuggestionService = (*multiSourceSuggestionService)(nil)

// multiSourceSuggestionService aggregates suggestions from various sources using fuzzy matching.
type multiSourceSuggestionService struct {
	historyMu sync.RWMutex
	history   []string
	tracker   ports.PromptTracker
	fs        persistence.FileSystem
}

// NewMultiSourceSuggestionService creates a new suggestion service and pre-loads the history.
func NewMultiSourceSuggestionService(fs persistence.FileSystem, tracker ports.PromptTracker, recentHistory []string) (ports.SuggestionService, error) {
	s := &multiSourceSuggestionService{
		history: make([]string, 0),
		tracker: tracker,
		fs:      fs,
	}

	// 1. Pre-load Global Top Prompts
	topPrompts, err := tracker.LoadTopN(context.Background(), 50)
	if err != nil {
		// Log error but continue with what we have
		fmt.Fprintf(os.Stderr, "Warning: failed to load top prompts: %v\n", err)
	}

	seen := make(map[string]bool)
	for _, p := range topPrompts {
		if !seen[p] {
			s.history = append(s.history, p)
			seen[p] = true
		}
	}

	// 2. Pre-load Active Session History
	for _, h := range recentHistory {
		if !seen[h] {
			s.history = append(s.history, h)
			seen[h] = true
		}
	}

	return s, nil
}

// GetSuggestions returns up to 10 suggestions based on fuzzy matching.
func (s *multiSourceSuggestionService) GetSuggestions(ctx context.Context, query string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	var suggestions []string

	s.historyMu.RLock()
	// If query is empty, return the first 5 items from history
	if query == "" {
		limit := min(5, len(s.history))
		suggestions = make([]string, limit)
		copy(suggestions, s.history[:limit])
		s.historyMu.RUnlock()
		return suggestions, nil
	}

	// 1. History Search
	for _, h := range s.history {
		if matcher.IsSubsequence(query, h) {
			suggestions = append(suggestions, h)
			if len(suggestions) >= 10 {
				break
			}
		}
	}
	s.historyMu.RUnlock()

	// 2. File System Search if it looks like a path
	if strings.Contains(query, string(os.PathSeparator)) || strings.Contains(query, "/") || strings.HasPrefix(query, ".") {
		fileSuggestions := s.scanFiles(ctx, query)
		suggestions = s.mergeSuggestions(suggestions, fileSuggestions, 10)
	}

	return suggestions, nil
}

// RecordPrompt records a user prompt into both the history and the global tracker.
func (s *multiSourceSuggestionService) RecordPrompt(prompt string) error {
	if prompt == "" {
		return nil
	}

	// 1. Immediate in-memory update
	s.historyMu.Lock()
	found := false
	for _, h := range s.history {
		if h == prompt {
			found = true
			break
		}
	}
	if !found {
		s.history = append(s.history, prompt)
	}
	s.historyMu.Unlock()

	// 2. Synchronous persistent update
	return s.tracker.Append(prompt)
}

func (s *multiSourceSuggestionService) scanFiles(ctx context.Context, query string) []string {
	dir, fileQuery := filepath.Split(query)
	if dir == "" {
		dir = "."
	}

	f, err := s.fs.Open(ctx, dir)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	var results []string
	for {
		// Yield to debouncer cancellation
		if err := ctx.Err(); err != nil {
			return results
		}

		// Read in chunks of 100 to avoid blocking during massive directory scans
		entries, err := f.ReadDir(100)
		if err != nil {
			if err == io.EOF {
				break
			}
			return results
		}

		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() && isIgnoredDir(name) {
				continue
			}

			if matcher.IsSubsequence(fileQuery, name) {
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

func (s *multiSourceSuggestionService) mergeSuggestions(s1, s2 []string, limit int) []string {
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

func isIgnoredDir(name string) bool {
	// 1. Explicitly ignored common dependency/build directories
	switch name {
	case ".git", "node_modules", "vendor", "bin", "obj":
		return true
	}

	// 2. Ignore hidden directories
	return strings.HasPrefix(name, ".")
}
