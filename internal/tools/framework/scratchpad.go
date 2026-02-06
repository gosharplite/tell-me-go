// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"context"
	"fmt"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/fsutil"
)

// ScratchpadStore manages a persistent scratchpad.
type ScratchpadStore struct {
	mu         sync.RWMutex
	scratchpad string
	filePath   string
	fs         fsutil.FileSystem
}

// NewScratchpadStore creates a new ScratchpadStore.
func NewScratchpadStore(fs fsutil.FileSystem, filePath string) *ScratchpadStore {
	return &ScratchpadStore{
		filePath: filePath,
		fs:       fs,
	}
}

// Load loads the scratchpad from disk.
func (s *ScratchpadStore) Load(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.fs.Stat(ctx, s.filePath); err != nil {
		return nil
	}

	data, err := s.fs.ReadFile(ctx, s.filePath)
	if err != nil {
		return err
	}

	s.scratchpad = string(data)
	return nil
}

// Save saves the scratchpad to disk.
func (s *ScratchpadStore) Save(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(ctx)
}

func (s *ScratchpadStore) saveLocked(ctx context.Context) error {
	data := []byte(s.scratchpad)
	return s.fs.WriteFile(ctx, s.filePath, data, 0644)
}

// ManageScratchpad handles the manage_scratchpad tool.
func (s *ScratchpadStore) ManageScratchpad(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	action, _ := args["action"].(string)
	content, _ := args["content"].(string)

	switch action {
	case "read":
		return s.read()
	case "write":
		return s.write(ctx, content)
	case "append":
		return s.append(ctx, content)
	case "clear":
		return s.clear(ctx)
	default:
		return tools.ToolResult{}, fmt.Errorf("unknown action: %s", action)
	}
}

func (s *ScratchpadStore) read() (tools.ToolResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.scratchpad == "" {
		return tools.ToolResult{Text: "(Scratchpad is empty)"}, nil
	}
	return tools.ToolResult{Text: s.scratchpad}, nil
}

func (s *ScratchpadStore) write(ctx context.Context, content string) (tools.ToolResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.scratchpad = content

	if err := s.saveLocked(ctx); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to save scratchpad: %w", err)
	}
	return tools.ToolResult{Text: "Scratchpad updated."}, nil
}

func (s *ScratchpadStore) append(ctx context.Context, content string) (tools.ToolResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.scratchpad != "" {
		s.scratchpad += "\n"
	}
	s.scratchpad += content

	if err := s.saveLocked(ctx); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to save scratchpad: %w", err)
	}
	return tools.ToolResult{Text: "Content appended to scratchpad."}, nil
}

func (s *ScratchpadStore) clear(ctx context.Context) (tools.ToolResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.scratchpad = ""

	if err := s.saveLocked(ctx); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to save scratchpad: %w", err)
	}
	return tools.ToolResult{Text: "Scratchpad cleared."}, nil
}
