// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package skills

import (
	"context"
	"sync"
	"testing"

	domain "github.com/gosharplite/tell-me-go/internal/domain/skills"
)

// TestFileSkillRepository_ConcurrentGetAllAndRefresh verifies that
// GetAll and Refresh can be called concurrently without data races.
// Run with: go test -race ./internal/infrastructure/skills/
func TestFileSkillRepository_ConcurrentGetAllAndRefresh(t *testing.T) {
	dir := t.TempDir()
	setupTestFiles(t, dir, map[string]string{
		"test-skill.md": "---\nname: test-skill\ndescription: A test skill\n---\nTest content body",
	})

	repo, err := NewFileSkillRepository(dir)
	if err != nil {
		t.Fatalf("NewFileSkillRepository: %v", err)
	}

	concurrentGetAllAndRefresh(t, repo)
}

// TestSkillsShRepository_ConcurrentGetAllAndRefresh verifies that
// GetAll and Refresh can be called concurrently without data races.
func TestSkillsShRepository_ConcurrentGetAllAndRefresh(t *testing.T) {
	dir := t.TempDir()
	setupTestFiles(t, dir, map[string]string{
		"owner-repo/skills/test-skill/SKILL.md": "---\nname: test-skill\ndescription: A test skill\n---\nTest content body",
	})

	repo, err := NewSkillsShRepository(dir)
	if err != nil {
		t.Fatalf("NewSkillsShRepository: %v", err)
	}

	concurrentGetAllAndRefresh(t, repo)
}

// concurrentGetAllAndRefresh spawns reader goroutines calling GetAll
// and writer goroutines calling Refresh concurrently, then waits for
// all to complete. Running under -race should detect any data races.
func concurrentGetAllAndRefresh(t *testing.T, repo domain.SkillRepository) {
	t.Helper()

	var wg sync.WaitGroup
	ctx := context.Background()

	// Readers: 10 goroutines, each calling GetAll 100 times
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, _ = repo.GetAll(ctx)
			}
		}()
	}

	// Writers: 3 goroutines, each calling Refresh 50 times
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = repo.Refresh(ctx)
			}
		}()
	}

	wg.Wait()
}
