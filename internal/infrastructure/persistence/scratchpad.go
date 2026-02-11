// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
)

// ScratchpadRepository manages a persistent scratchpad.
// It implements services.KVStore but is specialized for a single "content" key
// to maintain compatibility with raw text storage.
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

// Get retrieves the value for a key. Only "content" is supported for raw text storage.
func (r *ScratchpadRepository) Get(ctx context.Context, key string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if key != "content" {
		return "", nil
	}

	if _, err := r.fs.Stat(ctx, r.filePath); err != nil {
		return "", nil
	}

	data, err := r.fs.ReadFile(ctx, r.filePath)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// Set saves the value for a key. Only "content" is supported.
func (r *ScratchpadRepository) Set(ctx context.Context, key, val string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if key != "content" {
		return nil // Ignore other keys for now or return error? Let's stay compatible.
	}

	data := []byte(val)
	return r.fs.WriteFile(ctx, r.filePath, data, 0644)
}

// Delete clears the scratchpad.
func (r *ScratchpadRepository) Delete(ctx context.Context, key string) error {
	if key != "content" {
		return nil
	}
	return r.Set(ctx, key, "")
}

// GetAll returns all keys.
func (r *ScratchpadRepository) GetAll(ctx context.Context) (map[string]string, error) {
	val, err := r.Get(ctx, "content")
	if err != nil {
		return nil, err
	}
	return map[string]string{"content": val}, nil
}
