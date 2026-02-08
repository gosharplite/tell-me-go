// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
)

// ConfigStore manages persistent configuration settings.
type ConfigStore struct {
	mu       sync.RWMutex
	config   map[string]string
	filePath string
	fs       storage.FileSystem
}

// NewConfigStore creates a new ConfigStore.
func NewConfigStore(fs storage.FileSystem, filePath string) *ConfigStore {
	return &ConfigStore{
		config:   make(map[string]string),
		filePath: filePath,
		fs:       fs,
	}
}

// Load loads configuration from disk.
func (s *ConfigStore) Load(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.fs.Stat(ctx, s.filePath); err != nil {
		return nil
	}

	data, err := s.fs.ReadFile(ctx, s.filePath)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, &s.config)
}

// save saves configuration to disk.
func (s *ConfigStore) save(ctx context.Context) error {
	s.mu.RLock()
	data, err := json.MarshalIndent(s.config, "", "  ")
	s.mu.RUnlock()

	if err != nil {
		return err
	}
	return s.fs.WriteFile(ctx, s.filePath, data, 0644)
}

// GetAll returns a copy of all configuration.
func (s *ConfigStore) GetAll() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	res := make(map[string]string, len(s.config))
	for k, v := range s.config {
		res[k] = v
	}
	return res
}

// ManageConfig handles the manage_config tool.
func (s *ConfigStore) ManageConfig(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	action, _ := args["action"].(string)
	key, _ := args["key"].(string)
	val, _ := args["value"].(string)

	switch action {
	case "set":
		return s.set(ctx, key, val)
	case "get":
		return s.get(key)
	case "delete":
		return s.delete(ctx, key)
	case "list":
		return s.list()
	default:
		return tools.ToolResult{}, fmt.Errorf("unknown action: %s", action)
	}
}

func (s *ConfigStore) set(ctx context.Context, key, val string) (tools.ToolResult, error) {
	if key == "" {
		return tools.ToolResult{}, fmt.Errorf("key is required for set")
	}

	s.mu.Lock()
	s.config[key] = val
	s.mu.Unlock()

	if err := s.save(ctx); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to save config: %w", err)
	}
	return tools.ToolResult{Text: fmt.Sprintf("Config set: %s = %s", key, val)}, nil
}

func (s *ConfigStore) get(key string) (tools.ToolResult, error) {
	if key == "" {
		return tools.ToolResult{}, fmt.Errorf("key is required for get")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if v, ok := s.config[key]; ok {
		return tools.ToolResult{Text: v}, nil
	}
	return tools.ToolResult{}, fmt.Errorf("key not found: %s", key)
}

func (s *ConfigStore) delete(ctx context.Context, key string) (tools.ToolResult, error) {
	if key == "" {
		return tools.ToolResult{}, fmt.Errorf("key is required for delete")
	}

	s.mu.Lock()
	delete(s.config, key)
	s.mu.Unlock()

	if err := s.save(ctx); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to save config: %w", err)
	}
	return tools.ToolResult{Text: fmt.Sprintf("Config deleted: %s", key)}, nil
}

func (s *ConfigStore) list() (tools.ToolResult, error) {
	s.mu.RLock()
	var sb strings.Builder
	for k, v := range s.config {
		sb.WriteString(fmt.Sprintf("%s = %s\n", k, v))
	}
	s.mu.RUnlock()

	if sb.Len() == 0 {
		return tools.ToolResult{Text: "Configuration is empty."}, nil
	}
	return tools.ToolResult{Text: sb.String()}, nil
}
