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
	wg        sync.WaitGroup
	logger    io.Writer
}

// NewMultiSourceSuggestionService creates a new suggestion service and pre-loads the history.
func NewMultiSourceSuggestionService(fs persistence.FileSystem, tracker ports.PromptTracker, recentHistory []string, logger io.Writer) (ports.SuggestionService, error) {
	s := &multiSourceSuggestionService{
		history: make([]string, 0),
		tracker: tracker,
		fs:      fs,
		logger:  logger,
	}

	// 1. Pre-load Global Top Prompts
	topPrompts, err := tracker.LoadTopN(context.Background(), 50)
	if err != nil {
		// Log error but continue with what we have
		fmt.Fprintf(s.logger, "Warning: failed to load top prompts: %v\n", err)
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
func (s *multiSourceSuggestionService) RecordPrompt(ctx context.Context, prompt string) error {
	if prompt == "" {
		return nil
	}

	// 1. Immediate in-memory update
	s.historyMu.Lock()
	var updated []string
	updated = append(updated, prompt) // Prepend: Index 0 is the newest

	for _, h := range s.history {
		if h != prompt {
			updated = append(updated, h)
		}
	}

	// Scalability: Prevent unbounded memory growth over long sessions
	const maxHistory = 100
	if len(updated) > maxHistory {
		updated = updated[:maxHistory]
	}

	s.history = updated
	s.historyMu.Unlock()

	// 2. Asynchronous persistent update
	// We use context.WithoutCancel to ensure the write completes even if the request context is cancelled.
	// A goroutine is used to prevent blocking the UI thread.
	s.wg.Add(1)
	go func(ctx context.Context, p string) {
		defer s.wg.Done()
		// Use a detached context for the background write
		bgCtx := context.WithoutCancel(ctx)
		if err := s.tracker.Append(bgCtx, p); err != nil {
			// Since this is background, we can only log the error
			fmt.Fprintf(s.logger, "Warning: failed to record prompt to global tracker: %v\n", err)
		}
	}(ctx, prompt)

	return nil
}

// Close waits for all background tasks to finish.
func (s *multiSourceSuggestionService) Close(ctx context.Context) error {
	s.wg.Wait()
	return nil
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

		results = s.processEntriesBatch(entries, dir, fileQuery, results)
		if len(results) >= 10 {
			return results
		}
	}

	return results
}

func (s *multiSourceSuggestionService) processEntriesBatch(entries []os.DirEntry, dir string, fileQuery string, currentResults []string) []string {
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
			currentResults = append(currentResults, fullPath)
			if len(currentResults) >= 10 {
				return currentResults
			}
		}
	}
	return currentResults
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
