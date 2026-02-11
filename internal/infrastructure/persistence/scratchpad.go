// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
)

// ScratchpadRepository manages a persistent scratchpad.
type ScratchpadRepository struct {
	mu       sync.RWMutex
	filePath string
	fs       storage.FileSystem
}

// NewScratchpadRepository creates a new ScratchpadRepository.
func NewScratchpadRepository(fs storage.FileSystem, filePath string) *ScratchpadRepository {
	return &ScratchpadRepository{
		filePath: filePath,
		fs:       fs,
	}
}

// LoadScratchpad loads the scratchpad from disk.
func (r *ScratchpadRepository) LoadScratchpad(ctx context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, err := r.fs.Stat(ctx, r.filePath); err != nil {
		return "", nil
	}

	data, err := r.fs.ReadFile(ctx, r.filePath)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// SaveScratchpad saves the scratchpad to disk.
func (r *ScratchpadRepository) SaveScratchpad(ctx context.Context, content string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	data := []byte(content)
	return r.fs.WriteFile(ctx, r.filePath, data, 0644)
}
