// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package services

import (
	"context"
	"fmt"
	"sync"
)

// ConfigService handles the logic for managing configuration.
type ConfigService struct {
	mu     sync.RWMutex
	repo   ConfigRepository
	config map[string]string
}

// NewConfigService creates a new ConfigService.
func NewConfigService(repo ConfigRepository) *ConfigService {
	return &ConfigService{
		repo:   repo,
		config: make(map[string]string),
	}
}

// Initialize loads the configuration from the repository.
func (s *ConfigService) Initialize(ctx context.Context) error {
	config, err := s.repo.LoadConfig(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.config = config
	s.mu.Unlock()
	return nil
}

// Set sets a configuration key-value pair.
func (s *ConfigService) Set(ctx context.Context, key, val string) error {
	if key == "" {
		return fmt.Errorf("key is required for set")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Update local state first to prepare for save
	original := s.config[key]
	s.config[key] = val

	if err := s.repo.SaveConfig(ctx, s.config); err != nil {
		// Rollback on failure
		if original == "" {
			delete(s.config, key)
		} else {
			s.config[key] = original
		}
		return err
	}

	return nil
}

// Get returns the value for a configuration key.
func (s *ConfigService) Get(key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("key is required for get")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if v, ok := s.config[key]; ok {
		return v, nil
	}
	return "", fmt.Errorf("key not found: %s", key)
}

// Delete removes a configuration key.
func (s *ConfigService) Delete(ctx context.Context, key string) error {
	if key == "" {
		return fmt.Errorf("key is required for delete")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	val, ok := s.config[key]
	if !ok {
		return nil // Already deleted or doesn't exist
	}

	delete(s.config, key)

	if err := s.repo.SaveConfig(ctx, s.config); err != nil {
		// Rollback
		s.config[key] = val
		return err
	}

	return nil
}

// GetAll returns all configuration settings.
func (s *ConfigService) GetAll() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	res := make(map[string]string, len(s.config))
	for k, v := range s.config {
		res[k] = v
	}
	return res
}
