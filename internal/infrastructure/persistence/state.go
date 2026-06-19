// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	domain_persistence "github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
)

// sessionState manages all persistent services and session metadata.
type sessionState struct {
	Tasks     ports.TaskStore
	Settings  ports.KVStore
	Info      ports.SessionInfo
	db        *sql.DB
	statePath string
	fs        domain_persistence.FileSystem
	mu        sync.RWMutex
}

func (s *sessionState) GetTasks() ports.TaskStore  { return s.Tasks }
func (s *sessionState) GetSettings() ports.KVStore { return s.Settings }
func (s *sessionState) GetHealthChecker() ports.HealthChecker {
	if s.db != nil {
		return newSQLiteHealthChecker(s.db, s.Info.Paths["db_file"])
	}
	return &noOpHealthChecker{comp: ports.CompPersistence}
}
func (s *sessionState) GetInfo() ports.SessionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	copied := ports.SessionInfo{
		Model:    s.Info.Model,
		Provider: s.Info.Provider,
	}

	if s.Info.Env != nil {
		copied.Env = make(map[string]string, len(s.Info.Env))
		for k, v := range s.Info.Env {
			copied.Env[k] = v
		}
	}

	if s.Info.Paths != nil {
		copied.Paths = make(map[string]string, len(s.Info.Paths))
		for k, v := range s.Info.Paths {
			copied.Paths[k] = v
		}
	}

	if s.Info.ActiveToolkits != nil {
		copied.ActiveToolkits = make([]string, len(s.Info.ActiveToolkits))
		copy(copied.ActiveToolkits, s.Info.ActiveToolkits)
	}

	return copied
}

func (s *sessionState) SetInfo(info ports.SessionInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Info = info
	// Persist to disk
	if s.statePath != "" && s.Info.Env["STORAGE_TYPE"] != "memory" {
		data, err := json.MarshalIndent(s.Info, "", "  ")
		if err == nil {
			_ = s.fs.AtomicWrite(context.Background(), s.statePath, data, 0644)
		}
	}
}

// hydrateInfo loads persisted session info from disk (if available) and ensures
// all required fields have non-nil defaults.
func (s *sessionState) hydrateInfo(ctx context.Context, storageType string, repoPaths map[string]string) {
	if storageType != "memory" {
		if data, err := s.fs.ReadFile(ctx, s.statePath); err == nil {
			var loaded ports.SessionInfo
			if err := json.Unmarshal(data, &loaded); err == nil {
				s.Info = loaded
			}
		}
	}

	if s.Info.Env == nil {
		s.Info.Env = make(map[string]string)
	}
	s.Info.Env["STORAGE_TYPE"] = storageType

	if s.Info.Paths == nil {
		s.Info.Paths = make(map[string]string)
	}
	for k, v := range repoPaths {
		s.Info.Paths[k] = v
	}

	if s.Info.ActiveToolkits == nil {
		s.Info.ActiveToolkits = []string{}
	}
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
		return nil, fmt.Errorf("initializing persistence repositories: %w", err)
	}

	tasks, err := initServices(ctx, taskStore)
	if err != nil {
		return nil, err
	}

	fs := NewOSFileSystem()
	statePath := filepath.Join(configDir, "state.json")

	state := &sessionState{
		Tasks:     tasks,
		Settings:  kvStore,
		db:        db,
		statePath: statePath,
		fs:        fs,
	}

	state.hydrateInfo(ctx, storageType, paths)

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
	err = migrateFromJSON(ctx, db, fs, tasksPath, slog.Default())
	if err != nil {
		_ = db.Close()
		return nil, nil, nil, nil, err
	}

	return newSQLiteTaskStore(db),
		newSQLiteKVStore(db),
		db,
		paths, nil
}

func initServices(ctx context.Context, taskStore ports.ListStore[ports.Task]) (ports.TaskStore, error) {
	tasks := services.NewTaskService(taskStore)
	// Initialize is a no-op since taskService became stateless (Issue #906).
	// Kept for interface compatibility with ports.Initializer.
	_ = tasks.Initialize(ctx)
	return tasks, nil
}
