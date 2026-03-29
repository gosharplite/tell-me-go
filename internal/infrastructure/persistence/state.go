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
	Tasks    ports.TaskStore
	Settings ports.KVStore
	Info     ports.SessionInfo
	db       *sql.DB
}

func (s *sessionState) GetTasks() ports.TaskStore    { return s.Tasks }
func (s *sessionState) GetSettings() ports.KVStore { return s.Settings }
func (s *sessionState) GetInfo() ports.SessionInfo   { return s.Info }

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
func NewSessionState(ctx context.Context, configDir string) (ports.SessionProvider, error) {
	storageType := os.Getenv("STORAGE_TYPE")
	if storageType == "" {
		storageType = "sqlite" // Set sqlite as default storage
	}

	taskStore, kvStore, db, paths, err := initRepositories(ctx, configDir, storageType)
	if err != nil {
		return nil, err
	}

	tasks, err := initServices(ctx, taskStore)
	if err != nil {
		return nil, err
	}

	state := &sessionState{
		Tasks:    tasks,
		Settings: kvStore,
		db:       db,
	}

	state.Info = ports.SessionInfo{
		Env: map[string]string{
			"STORAGE_TYPE": storageType,
		},
		Paths: paths,
	}

	return state, nil
}

func initRepositories(ctx context.Context, configDir, storageType string) (ports.ListStore[ports.Task], ports.KVStore, *sql.DB, map[string]string, error) {
	paths := map[string]string{"config_dir": configDir}

	if storageType == "memory" {
		return newMemoryListStore[ports.Task](), newMemoryKVStore(), nil, paths, nil
	}

	fs := NewOSFileSystem()
	tasksPath := filepath.Join(configDir, "tasks.json")

	// Legacy flat file storage is completely obsolete for active use.
	// Only SQLite and Memory are supported for full operations.
	// SQLite implementation
	dbPath := filepath.Join(configDir, "tellmego.db")
	paths["db_file"] = dbPath

	db, err := initSQLiteDB(ctx, dbPath) // We will pass ctx here!
	if err != nil {
		return nil, nil, nil, nil, err
	}

	// Perform migration if needed
	err = migrateFromJSON(ctx, db, fs, tasksPath)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	return newSQLiteTaskStore(db),
		newSQLiteKVStore(db),
		db,
		paths, nil
}

func initServices(ctx context.Context, taskStore ports.ListStore[ports.Task]) (ports.TaskStore, error) {
	tasks := services.NewTaskService(taskStore)

	if err := tasks.Initialize(ctx); err != nil {
		return nil, err
	}
	return tasks, nil
}
