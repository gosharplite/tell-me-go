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
	storageType := os.Getenv("STORAGE_TYPE")
	if storageType == "" {
		storageType = "file"
	}

	var taskStore services.ListStore[services.Task]
	var configStore services.KVStore
	var scratchStore services.KVStore
	var scratchPath string
	var tasksPath string
	var configPath string

	if storageType == "memory" {
		taskStore = NewMemoryListStore[services.Task]()
		configStore = NewMemoryKVStore()
		scratchStore = NewMemoryKVStore()
	} else {
		fs := storage.DefaultFileSystem
		tasksPath = filepath.Join(configDir, "tasks.json")
		configPath = filepath.Join(configDir, "config.json")
		scratchPath = filepath.Join(configDir, "scratchpad.md")

		taskStore = NewTaskRepository(fs, tasksPath)
		configStore = NewConfigRepository(fs, configPath)
		scratchStore = NewScratchpadRepository(fs, scratchPath)
	}

	tasks := services.NewTaskService(taskStore)
	config := services.NewConfigService(configStore)
	scratch := services.NewScratchpadService(scratchStore)

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
			"STORAGE_TYPE": storageType,
		},
		Paths: map[string]string{
			"config_dir":   configDir,
			"scratch_file": scratchPath,
			"tasks_file":   tasksPath,
			"config_file":  configPath,
		},
	}

	return state, nil
}
