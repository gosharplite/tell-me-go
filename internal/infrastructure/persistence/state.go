// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
)

// sessionState manages all persistent services and session metadata.
type sessionState struct {
	Tasks      ports.TaskService
	Config     *services.ConfigService
	Scratchpad *services.ScratchpadService
	Info       ports.SessionInfo
	db         *sql.DB
}

func (s *sessionState) GetTasks() ports.TaskService             { return s.Tasks }
func (s *sessionState) GetConfig() ports.IConfigService         { return s.Config }
func (s *sessionState) GetScratchpad() ports.IScratchpadService { return s.Scratchpad }
func (s *sessionState) GetInfo() ports.SessionInfo              { return s.Info }

func (s *sessionState) SetInfo(info ports.SessionInfo) {
	s.Info = info
}

func (s *sessionState) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// NewSessionState initializes repositories and services.
func NewSessionState(ctx context.Context, configDir string) (ports.ISessionProvider, error) {
	storageType := os.Getenv("STORAGE_TYPE")
	if storageType == "" {
		storageType = "sqlite" // Set sqlite as default storage
	}

	taskStore, configStore, scratchStore, db, paths, err := initRepositories(ctx, configDir, storageType)
	if err != nil {
		return nil, err
	}

	tasks, config, scratch, err := initServices(ctx, taskStore, configStore, scratchStore)
	if err != nil {
		return nil, err
	}

	state := &sessionState{
		Tasks:      tasks,
		Config:     config,
		Scratchpad: scratch,
		db:         db,
	}

	state.Info = ports.SessionInfo{
		Config: config.GetAll(),
		Env: map[string]string{
			"TELL_ME_MODE": os.Getenv("TELL_ME_MODE"),
			"STORAGE_TYPE": storageType,
		},
		Paths: paths,
	}

	return state, nil
}

func initRepositories(ctx context.Context, configDir, storageType string) (ports.ListStore[ports.Task], ports.KVStore, ports.KVStore, *sql.DB, map[string]string, error) {
	paths := map[string]string{"config_dir": configDir}

	if storageType == "memory" {
		return newMemoryListStore[ports.Task](), newMemoryKVStore(), newMemoryKVStore(), nil, paths, nil
	}

	fs := NewOSFileSystem()
	tasksPath := filepath.Join(configDir, "tasks.json")
	configPath := filepath.Join(configDir, "config.json")
	scratchPath := filepath.Join(configDir, "scratchpad.md")

	// Legacy flat file storage is completely obsolete for active use.
	// Only SQLite and Memory are supported for full operations.
	// SQLite implementation
	dbPath := filepath.Join(configDir, "tellmego.db")
	paths["db_file"] = dbPath

	db, err := initSQLiteDB(ctx, dbPath) // We will pass ctx here!
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	// Perform migration if needed
	err = migrateFromJSON(ctx, db, fs, tasksPath, configPath, scratchPath)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	return newSQLiteTaskStore(db),
		newSQLiteConfigStore(db),
		newSQLiteScratchpadStore(db),
		db,
		paths, nil
}

func initServices(ctx context.Context, taskStore ports.ListStore[ports.Task], configStore, scratchStore ports.KVStore) (ports.TaskService, *services.ConfigService, *services.ScratchpadService, error) {
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
