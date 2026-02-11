// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"os"
	"path/filepath"

	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
)

// SessionState manages all persistent services and session metadata.
type SessionState struct {
	Tasks      *services.TaskService
	Config     *services.ConfigService
	Scratchpad *services.ScratchpadService
	Info       SessionInfo
}

// SessionInfo holds metadata about the current execution environment.
type SessionInfo struct {
	Config map[string]string `json:"config"`
	Env    map[string]string `json:"env"`
	Paths  map[string]string `json:"paths"`
}

// NewSessionState initializes repositories and services.
func NewSessionState(ctx context.Context, configDir string) (*SessionState, error) {
	fs := storage.DefaultFileSystem

	taskRepo := NewTaskRepository(fs, filepath.Join(configDir, "tasks.json"))
	configRepo := NewConfigRepository(fs, filepath.Join(configDir, "config.json"))
	scratchRepo := NewScratchpadRepository(fs, filepath.Join(configDir, "scratchpad.md"))

	tasks := services.NewTaskService(taskRepo)
	config := services.NewConfigService(configRepo)
	scratch := services.NewScratchpadService(scratchRepo)

	if err := tasks.Initialize(ctx); err != nil {
		return nil, err
	}
	if err := config.Initialize(ctx); err != nil {
		return nil, err
	}
	if err := scratch.Initialize(ctx); err != nil {
		return nil, err
	}

	state := &SessionState{
		Tasks:      tasks,
		Config:     config,
		Scratchpad: scratch,
	}

	state.Info = SessionInfo{
		Config: config.GetAll(),
		Env: map[string]string{
			"TELL_ME_MODE": os.Getenv("TELL_ME_MODE"),
		},
		Paths: map[string]string{
			"config_dir":   configDir,
			"scratch_file": scratchRepo.filePath,
			"tasks_file":   taskRepo.filePath,
			"config_file":  configRepo.filePath,
		},
	}

	return state, nil
}
