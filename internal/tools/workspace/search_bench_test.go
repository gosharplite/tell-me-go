// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"strings"
	"testing"

	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
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
	fs := persistencetest.NewPlainOSFileSystem()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithCancel(ctx)
		resChan, errChan := ConcurrentSearch(ctx, sp, fs, ".", nil, func(_, line string) (string, bool) {
			return "", strings.Contains(line, "func ")
		}, infra_persistence.NewWorkspacePolicy())

		var results []string
		for res := range resChan {
			if len(results) >= 1000 {
				cancel()
				break
			}
			results = append(results, res)
		}
		cancel()

		var finalErr error
		select {
		case err := <-errChan:
			finalErr = err
		default:
		}

		if finalErr != nil {
			b.Fatal(finalErr)
		}
	}
}

func BenchmarkConcurrentSearch_EarlyStop(b *testing.B) {
	ctx := context.Background()
	sp := &benchmarkSearchSecurityManager{}
	fs := persistencetest.NewPlainOSFileSystem()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithCancel(ctx)
		resChan, errChan := ConcurrentSearch(ctx, sp, fs, ".", nil, func(_, line string) (string, bool) {
			return "", strings.Contains(line, "func ")
		}, infra_persistence.NewWorkspacePolicy())

		var results []string
		for res := range resChan {
			if len(results) >= 1 {
				cancel()
				break
			}
			results = append(results, res)
		}
		cancel()

		var finalErr error
		select {
		case err := <-errChan:
			finalErr = err
		default:
		}

		if finalErr != nil {
			b.Fatal(finalErr)
		}
	}
}
