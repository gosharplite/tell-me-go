// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package app

import (
	"context"
	"io"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
)

type mockPromptTracker struct {
	loadTopNFunc func(ctx context.Context, limit int) ([]string, error)
	appendFunc   func(ctx context.Context, prompt string) error
	closeFunc    func() error
}

var _ ports.PromptTracker = (*mockPromptTracker)(nil)

func (m *mockPromptTracker) Append(ctx context.Context, prompt string) error {
	if m.appendFunc != nil {
		return m.appendFunc(ctx, prompt)
	}
	return nil
}

func (m *mockPromptTracker) LoadTopN(ctx context.Context, limit int) ([]string, error) {
	if m.loadTopNFunc != nil {
		return m.loadTopNFunc(ctx, limit)
	}
	return nil, nil
}

func (m *mockPromptTracker) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

type mockFileSystem struct {
	persistence.FileSystem
}

var _ persistence.FileSystem = (*mockFileSystem)(nil)

type mockWorkspacePolicy struct {
	shouldIgnoreDirFunc  func(name string) bool
	shouldIgnorePathFunc func(path string) bool
}

var _ services.WorkspacePolicy = (*mockWorkspacePolicy)(nil)

func (m *mockWorkspacePolicy) ShouldIgnoreDir(name string) bool {
	if m.shouldIgnoreDirFunc != nil {
		return m.shouldIgnoreDirFunc(name)
	}
	return false
}

func (m *mockWorkspacePolicy) ShouldIgnorePath(path string) bool {
	if m.shouldIgnorePathFunc != nil {
		return m.shouldIgnorePathFunc(path)
	}
	return false
}

func TestBuildSuggestionService_Delegation(t *testing.T) {
	ctx := context.Background()
	fs := &mockFileSystem{}
	tracker := &mockPromptTracker{
		loadTopNFunc: func(ctx context.Context, limit int) ([]string, error) {
			return []string{"git status", "git diff"}, nil
		},
	}
	wp := &mockWorkspacePolicy{}
	recentHistory := []string{"go test ./..."}

	service, err := BuildSuggestionService(ctx, fs, tracker, recentHistory, io.Discard, wp)
	if err != nil {
		t.Fatalf("BuildSuggestionService failed: %v", err)
	}
	if service == nil {
		t.Fatal("expected non-nil suggestion service")
	}

	suggestions, err := service.GetSuggestions(ctx, "")
	if err != nil {
		t.Fatalf("GetSuggestions failed: %v", err)
	}
	if len(suggestions) < 3 {
		t.Fatalf("expected at least 3 suggestions, got %v", suggestions)
	}
	if suggestions[0] != "git status" || suggestions[1] != "git diff" || suggestions[2] != "go test ./..." {
		t.Errorf("unexpected suggestions: %v", suggestions)
	}
}
