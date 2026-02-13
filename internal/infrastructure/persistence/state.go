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

// sessionState manages all persistent services and session metadata.
type sessionState struct {
	Tasks      *services.TaskService
	Config     *services.ConfigService
	Scratchpad *services.ScratchpadService
	Info       sessionInfo
}

func (s *sessionState) GetTasks() *services.TaskService            { return s.Tasks }
func (s *sessionState) GetConfig() *services.ConfigService         { return s.Config }
func (s *sessionState) GetScratchpad() *services.ScratchpadService { return s.Scratchpad }
func (s *sessionState) GetInfo() any                               { return s.Info }

// sessionInfo holds metadata about the current execution environment.
type sessionInfo struct {
	Config map[string]string `json:"config"`
	Env    map[string]string `json:"env"`
	Paths  map[string]string `json:"paths"`
}

// NewSessionState initializes repositories and services.
func NewSessionState(ctx context.Context, configDir string) (services.ISessionProvider, error) {
	storageType := os.Getenv("STORAGE_TYPE")
	if storageType == "" {
		storageType = "file"
	}

	taskStore, configStore, scratchStore, paths := initRepositories(configDir, storageType)

	tasks, config, scratch, err := initServices(ctx, taskStore, configStore, scratchStore)
	if err != nil {
		return nil, err
	}

	state := &sessionState{
		Tasks:      tasks,
		Config:     config,
		Scratchpad: scratch,
	}

	state.Info = sessionInfo{
		Config: config.GetAll(),
		Env: map[string]string{
			"TELL_ME_MODE": os.Getenv("TELL_ME_MODE"),
			"STORAGE_TYPE": storageType,
		},
		Paths: paths,
	}

	return state, nil
}

func initRepositories(configDir, storageType string) (services.ListStore[services.Task], services.KVStore, services.KVStore, map[string]string) {
	paths := map[string]string{"config_dir": configDir}

	if storageType == "memory" {
		return newMemoryListStore[services.Task](), newMemoryKVStore(), newMemoryKVStore(), paths
	}

	fs := storage.DefaultFileSystem
	tasksPath := filepath.Join(configDir, "tasks.json")
	configPath := filepath.Join(configDir, "config.json")
	scratchPath := filepath.Join(configDir, "scratchpad.md")

	paths["tasks_file"] = tasksPath
	paths["config_file"] = configPath
	paths["scratch_file"] = scratchPath

	return newTaskRepository(fs, tasksPath),
		newConfigRepository(fs, configPath),
		newScratchpadRepository(fs, scratchPath),
		paths
}

func initServices(ctx context.Context, taskStore services.ListStore[services.Task], configStore, scratchStore services.KVStore) (*services.TaskService, *services.ConfigService, *services.ScratchpadService, error) {
	tasks := services.NewTaskService(taskStore)
	config := services.NewConfigService(configStore)
	scratch := services.NewScratchpadService(scratchStore)

	if err := tasks.Initialize(ctx); err != nil {
		return nil, nil, nil, err
	}
	if err := config.Initialize(ctx); err != nil {
		return nil, nil, nil, err
	}
	if err := scratch.Initialize(ctx); err != nil {
		return nil, nil, nil, err
	}
	return tasks, config, scratch, nil
}
