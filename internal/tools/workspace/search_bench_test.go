// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
)

type benchmarkSearchSecurityManager struct {
	mockSP
}

func (s *benchmarkSearchSecurityManager) IsPathSafe(path string) (string, error) {
	return path, nil
}

func BenchmarkConcurrentSearch_FullProject(b *testing.B) {
	ctx := context.Background()
	sp := &benchmarkSearchSecurityManager{}
	fs := persistence.DefaultFileSystem

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := ConcurrentSearch(ctx, sp, fs, ".", func(_, line string) bool {
			return strings.Contains(line, "func ")
		}, 1000)
		if err != nil && err.Error() != "too many results" {
			b.Fatal(err)
		}
	}
}

func BenchmarkConcurrentSearch_EarlyStop(b *testing.B) {
	ctx := context.Background()
	sp := &benchmarkSearchSecurityManager{}
	fs := persistence.DefaultFileSystem

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := ConcurrentSearch(ctx, sp, fs, ".", func(_, line string) bool {
			return strings.Contains(line, "func ")
		}, 1)
		if err != nil && err.Error() != "too many results" {
			b.Fatal(err)
		}
	}
}
