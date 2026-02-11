// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
)

// ConfigRepository manages persistent configuration settings.
type ConfigRepository struct {
	mu       sync.RWMutex
	filePath string
	fs       storage.FileSystem
}

// NewConfigRepository creates a new ConfigRepository.
func NewConfigRepository(fs storage.FileSystem, filePath string) *ConfigRepository {
	return &ConfigRepository{
		filePath: filePath,
		fs:       fs,
	}
}

// LoadConfig loads configuration from disk.
func (r *ConfigRepository) LoadConfig(ctx context.Context) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, err := r.fs.Stat(ctx, r.filePath); err != nil {
		return make(map[string]string), nil
	}

	data, err := r.fs.ReadFile(ctx, r.filePath)
	if err != nil {
		return nil, err
	}

	config := make(map[string]string)
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return config, nil
}

// SaveConfig saves configuration to disk.
func (r *ConfigRepository) SaveConfig(ctx context.Context, config map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return r.fs.WriteFile(ctx, r.filePath, data, 0644)
}
