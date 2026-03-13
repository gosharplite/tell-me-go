// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors for repository operations.
var (
	ErrConfigKeyNotFound = errors.New("config key not found")
	ErrTaskNotFound      = errors.New("task not found")
)

// KVStore defines a generic key-value storage interface.
type KVStore interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, val string) error
	Delete(ctx context.Context, key string) error
	GetAll(ctx context.Context) (map[string]string, error)
}

// ListStore defines a generic list storage interface.
type ListStore[T any] interface {
	ReadAll(ctx context.Context) ([]T, error)
	Append(ctx context.Context, item T) error
	Update(ctx context.Context, id float64, item T) error
	Delete(ctx context.Context, id float64) error
	DeleteAll(ctx context.Context) error
}

// Task represents a unit of work in the to-do list.
type Task struct {
	ID        float64   `json:"id"`
	Content   string    `json:"content"`
	Status    string    `json:"status"` // pending, completed
	CreatedAt time.Time `json:"created_at"`
}

// SessionInfo holds metadata about the current execution environment.
type SessionInfo struct {
	Config   map[string]string `json:"config"`
	Env      map[string]string `json:"env"`
	Paths    map[string]string `json:"paths"`
	Model    string            `json:"model,omitempty"`
	Provider string            `json:"provider,omitempty"`
}

// TaskReader defines the interface for reading tasks.
type TaskReader interface {
	ListTasks(status string) []Task
}

// TaskWriter defines the interface for modifying tasks.
type TaskWriter interface {
	AddTask(ctx context.Context, content string) (Task, error)
	UpdateTask(ctx context.Context, id float64, content, status string) (Task, error)
	DeleteTask(ctx context.Context, id float64) error
	ClearTasks(ctx context.Context) error
}

// ITaskService defines the interface for task management.
type ITaskService interface {
	TaskReader
	TaskWriter
	Initialize(ctx context.Context) error
}

// ConfigReader defines the interface for reading configuration.
type ConfigReader interface {
	Get(key string) (string, error)
	GetAll() map[string]string
}

// ConfigWriter defines the interface for modifying configuration.
type ConfigWriter interface {
	Set(ctx context.Context, key, val string) error
	Delete(ctx context.Context, key string) error
}

// IConfigService defines the interface for configuration management.
type IConfigService interface {
	ConfigReader
	ConfigWriter
	Initialize(ctx context.Context) error
}

// ScratchpadReader defines the interface for reading from the scratchpad.
type ScratchpadReader interface {
	Read() string
}

// ScratchpadWriter defines the interface for writing to the scratchpad.
type ScratchpadWriter interface {
	Write(ctx context.Context, content string) error
	Append(ctx context.Context, content string) error
	Clear(ctx context.Context) error
}

// IScratchpadService defines the interface for scratchpad management.
type IScratchpadService interface {
	ScratchpadReader
	ScratchpadWriter
	Initialize(ctx context.Context) error
}

// PersistenceProvider provides access to domain-specific persistence services.
type PersistenceProvider interface {
	GetTasks() ITaskService
	GetConfig() IConfigService
	GetScratchpad() IScratchpadService
}

// SessionStateProvider manages session-level metadata and state.
type SessionStateProvider interface {
	GetInfo() SessionInfo
	SetInfo(info SessionInfo)
}

// ResourceCloser defines an interface for closing resources.
type ResourceCloser interface {
	Close() error
}

// ISessionProvider provides access to persistence services and session info.
type ISessionProvider interface {
	PersistenceProvider
	SessionStateProvider
	ResourceCloser
}
