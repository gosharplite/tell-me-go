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
	ErrTaskNotFound = errors.New("task not found")
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
	Env            map[string]string `json:"env"`
	Paths          map[string]string `json:"paths"`
	Model          string            `json:"model,omitempty"`
	Provider       string            `json:"provider,omitempty"`
	ActiveToolkits []string          `json:"active_toolkits,omitempty"` // Tracks lazy-loaded domains
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

// TaskStore defines the interface for task management.
type TaskStore interface {
	TaskReader
	TaskWriter
}

// Initializer defines an interface for components requiring lifecycle initialization.
type Initializer interface {
	Initialize(ctx context.Context) error
}

// PersistenceProvider provides access to domain-specific persistence services.
type PersistenceProvider interface {
	GetTasks() TaskStore
	GetSettings() KVStore
	GetHealthChecker() HealthChecker
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

// SessionProvider provides access to persistence services and session info.
type SessionProvider interface {
	PersistenceProvider
	SessionStateProvider
	ResourceCloser
}
