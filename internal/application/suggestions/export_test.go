// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package suggestions

import (
	"context"
	"io"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
)

// Export for testing
type MultiSourceSuggestionServiceInternal = multiSourceSuggestionService

func (s *multiSourceSuggestionService) ScanFiles(ctx context.Context, prefix string) []string {
	return s.scanFiles(ctx, prefix)
}

func (s *multiSourceSuggestionService) MergeSuggestions(s1, s2 []string, limit int) []string {
	return s.mergeSuggestions(s1, s2, limit)
}

func (s *multiSourceSuggestionService) GetHistory() []string {
	return s.history
}

func NewInternalService(logger io.Writer) *MultiSourceSuggestionServiceInternal {
	return &multiSourceSuggestionService{logger: logger}
}

func (s *multiSourceSuggestionService) AddWork(n int) {
	s.wg.Add(n)
}

func (s *multiSourceSuggestionService) SetFS(fs persistence.FileSystem) {
	s.fs = fs
}
