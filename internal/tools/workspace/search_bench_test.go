// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Benchmark stability (7-run CV measured on 2026-05-21):
//   In-memory: CV < 8%   (SyntheticScale 7.7%, Stress_100KFiles 5.2%)
//   Real I/O:  CV < 18%  (EarlyStop 17.3%, FullProject 10.1%, IOScale 4.6%)
// Run with: go test -bench=. -benchmem -count=7 -benchtime=500ms

package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	domain_persistence "github.com/gosharplite/tell-me-go/internal/domain/persistence"
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

	b.ReportAllocs()
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

		if err := drainErrChan(errChan); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConcurrentSearch_EarlyStop(b *testing.B) {
	ctx := context.Background()
	sp := &benchmarkSearchSecurityManager{}
	fs := persistencetest.NewPlainOSFileSystem()

	b.ReportAllocs()
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

		if err := drainErrChan(errChan); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConcurrentSearch_SyntheticScale(b *testing.B) {
	ctx := context.Background()
	sp := &benchmarkSearchSecurityManager{}

	// Build a 100MB+ in-memory workspace of small files.
	// 10,000 files × 10KB = ~100MB. Each file has ~200 lines
	// of realistic-looking code.
	const numFiles = 10000
	const linesPerFile = 200
	fs := domain_persistence.NewMockFileSystem()

	for i := 0; i < numFiles; i++ {
		var sb strings.Builder
		sb.WriteString("package generated\n\n")
		for j := 0; j < linesPerFile; j++ {
			fmt.Fprintf(&sb, "func generated_%d_%d(x int) int { return x + %d }\n", i, j, j)
		}
		if err := fs.WriteFile(context.Background(),
			fmt.Sprintf("src/pkg_%d/file_%d.go", i%100, i),
			[]byte(sb.String()), 0644); err != nil {
			b.Fatal(err)
		}
	}

	// Search for a rare pattern to exercise the full scan
	matcher := func(_ string, line string) (string, bool) {
		return "", strings.Contains(line, "func generated_5000_50")
	}

	policy := infra_persistence.NewWorkspacePolicy()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithCancel(ctx)
		resChan, errChan := ConcurrentSearch(ctx, sp, fs, ".", nil, matcher, policy)

		var count int
		for range resChan {
			count++
			if count >= 10 {
				cancel()
				break
			}
		}
		cancel()

		if err := drainErrChan(errChan); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConcurrentSearch_IOScale(b *testing.B) {
	ctx := context.Background()
	sp := &benchmarkSearchSecurityManager{}

	tmpDir := b.TempDir()

	// Generate ~100MB of small files on real disk
	const numDirs = 100
	const filesPerDir = 100
	lineTemplate := "func generated_%d_%d_%d(x int) int { return x + %d }\n"

	for d := 0; d < numDirs; d++ {
		dirPath := filepath.Join(tmpDir, fmt.Sprintf("pkg_%d", d))
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			b.Fatal(err)
		}
		for f := 0; f < filesPerDir; f++ {
			filePath := filepath.Join(dirPath, fmt.Sprintf("file_%d.go", f))
			fh, err := os.Create(filePath)
			if err != nil {
				b.Fatal(err)
			}
			if _, err := fmt.Fprintf(fh, "package pkg_%d\n\n", d); err != nil {
				_ = fh.Close()
				b.Fatal(err)
			}
			for line := 0; line < 200; line++ {
				if _, err := fmt.Fprintf(fh, lineTemplate, d, f, line, line); err != nil {
					_ = fh.Close()
					b.Fatal(err)
				}
			}
			if err := fh.Close(); err != nil {
				b.Fatal(err)
			}
		}
	}

	fs := persistencetest.NewPlainOSFileSystem()
	policy := infra_persistence.NewWorkspacePolicy()
	matcher := func(_ string, line string) (string, bool) {
		return "", strings.Contains(line, "func generated_50_50_50")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithCancel(ctx)
		resChan, errChan := ConcurrentSearch(ctx, sp, fs, tmpDir, nil, matcher, policy)

		var count int
		for range resChan {
			count++
			if count >= 1 {
				cancel()
				break
			}
		}
		cancel()

		if err := drainErrChan(errChan); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkConcurrentSearch_Stress_100KFiles measures search throughput
// with 100,000 small files (~1KB each, ~100MB total). The bottleneck is
// directory traversal (Walk + Stat + Open) rather than line scanning,
// simulating a large monorepo with many small source files.
func BenchmarkConcurrentSearch_Stress_100KFiles(b *testing.B) {
	ctx := context.Background()
	sp := &benchmarkSearchSecurityManager{}

	const numFiles = 100000
	const linesPerFile = 20
	fs := domain_persistence.NewMockFileSystem()

	for i := 0; i < numFiles; i++ {
		var sb strings.Builder
		sb.WriteString("package generated\n\n")
		for j := 0; j < linesPerFile; j++ {
			fmt.Fprintf(&sb, "func gen_%d_%d(x int) int { return x + %d }\n", i, j, j)
		}
		if err := fs.WriteFile(context.Background(),
			fmt.Sprintf("src/pkg_%d/file_%d.go", i%200, i),
			[]byte(sb.String()), 0644); err != nil {
			b.Fatal(err)
		}
	}

	// Search for a pattern that appears exactly once (in the last file).
	matcher := func(_ string, line string) (string, bool) {
		return "", strings.Contains(line, "func gen_99999_10")
	}

	policy := infra_persistence.NewWorkspacePolicy()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithCancel(ctx)
		resChan, errChan := ConcurrentSearch(ctx, sp, fs, ".", nil, matcher, policy)

		var count int
		for range resChan {
			count++
			if count >= 1 {
				cancel()
				break
			}
		}
		cancel()

		if err := drainErrChan(errChan); err != nil {
			b.Fatal(err)
		}
	}
}

// drainErrChan reads a single error from the error channel, if available.
// The channel must be buffered (capacity ≥ 1) for non-blocking read.
func drainErrChan(errChan <-chan error) error {
	select {
	case err := <-errChan:
		return err
	default:
		return nil
	}
}
