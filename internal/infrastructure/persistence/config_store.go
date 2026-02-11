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
// It implements services.KVStore.
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

// GetAll loads all configuration from disk.
func (r *ConfigRepository) GetAll(ctx context.Context) (map[string]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

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

// Get retrieves a single key.
func (r *ConfigRepository) Get(ctx context.Context, key string) (string, error) {
	config, err := r.GetAll(ctx)
	if err != nil {
		return "", err
	}
	return config[key], nil
}

// Set updates a single key and saves to disk.
func (r *ConfigRepository) Set(ctx context.Context, key, val string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	config := make(map[string]string)
	if _, err := r.fs.Stat(ctx, r.filePath); err == nil {
		data, err := r.fs.ReadFile(ctx, r.filePath)
		if err == nil {
			_ = json.Unmarshal(data, &config)
		}
	}

	config[key] = val

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return r.fs.WriteFile(ctx, r.filePath, data, 0644)
}

// Delete removes a key and saves to disk.
func (r *ConfigRepository) Delete(ctx context.Context, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, err := r.fs.Stat(ctx, r.filePath); err != nil {
		return nil
	}

	data, err := r.fs.ReadFile(ctx, r.filePath)
	if err != nil {
		return err
	}

	config := make(map[string]string)
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}

	delete(config, key)

	data, err = json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return r.fs.WriteFile(ctx, r.filePath, data, 0644)
}
