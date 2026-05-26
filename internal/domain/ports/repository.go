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

// ListFilter provides optional filtering for ListStore.Query.
// All fields are optional — zero values mean "no filter."
type ListFilter struct {
	Status    string    // empty = all statuses; non-empty = exact match
	NotStatus string    // empty = no exclusion; non-empty = exclude this status
	Since     time.Time // zero = no lower bound on CreatedAt
	Before    time.Time // zero = no upper bound on CreatedAt
}

// ListStore defines a generic list storage interface.
type ListStore[T any] interface {
	ReadAll(ctx context.Context) ([]T, error)

	// Query returns items matching the filter, bounded by limit and offset.
	// limit=0 means no limit; offset=0 means start from beginning.
	Query(ctx context.Context, filter ListFilter, limit, offset int) ([]T, error)

	// Count returns the total number of items in the store.
	Count(ctx context.Context) (int, error)

	Append(ctx context.Context, item T) error
	Update(ctx context.Context, id int64, item T) error
	Delete(ctx context.Context, id int64) error
	DeleteAll(ctx context.Context) error
}

// Task represents a unit of work in the to-do list.
type Task struct {
	ID        int64     `json:"id"`
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
	// ListTasks returns tasks filtered by status, bounded by limit and offset.
	// status="" returns all statuses. limit=0 means no limit. offset=0 means start from beginning.
	ListTasks(status string, limit, offset int) []Task

	// CountTasks returns the total number of tasks matching the given status filter.
	// status="" returns the total count across all statuses.
	CountTasks(status string) int
}

// TaskWriter defines the interface for modifying tasks.
type TaskWriter interface {
	AddTask(ctx context.Context, content string) (Task, error)
	UpdateTask(ctx context.Context, id int64, content, status string) (Task, error)
	DeleteTask(ctx context.Context, id int64) error
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
