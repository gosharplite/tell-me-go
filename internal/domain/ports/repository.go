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

// KVStore defines a generic key-value storage interface for persisting
// simple configuration and settings data.
type KVStore interface {
	// Get retrieves the value for the given key. Returns an empty
	// string if the key does not exist. The implementation determines
	// whether a missing key is an error.
	Get(ctx context.Context, key string) (string, error)

	// Set stores a value for the given key, overwriting any existing
	// value. The change is immediately durable.
	Set(ctx context.Context, key string, val string) error

	// Delete removes the given key and its value. It is a no-op if
	// the key does not exist.
	Delete(ctx context.Context, key string) error

	// GetAll returns a copy of all key-value pairs currently stored.
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

// ListStore defines a generic list storage interface for ordered,
// filterable collections of items.
type ListStore[T any] interface {
	// ReadAll returns every item in the store in insertion order.
	ReadAll(ctx context.Context) ([]T, error)

	// Query returns items matching the filter, bounded by limit and offset.
	// limit=0 means no limit; offset=0 means start from beginning.
	Query(ctx context.Context, filter ListFilter, limit, offset int) ([]T, error)

	// Count returns the total number of items in the store.
	Count(ctx context.Context) (int, error)

	// Append adds an item to the end of the store.
	Append(ctx context.Context, item T) error

	// Update replaces the item at the given zero-based index.
	Update(ctx context.Context, id int64, item T) error

	// Delete removes the item at the given zero-based index.
	Delete(ctx context.Context, id int64) error

	// DeleteAll removes every item from the store.
	DeleteAll(ctx context.Context) error
}

// Task represents a unit of work in the to-do list.
type Task struct {
	// ID is the unique, monotonically increasing identifier.
	ID int64 `json:"id"`
	// Content is the human-readable description of the task.
	Content string `json:"content"`
	// Status is the current state: "pending" or "completed".
	Status string `json:"status"`
	// CreatedAt records when the task was added.
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
	// AddTask creates a new task with the given content and "pending" status.
	// Returns the created task with its assigned ID.
	AddTask(ctx context.Context, content string) (Task, error)

	// UpdateTask modifies the content and/or status of an existing task.
	// An empty content string leaves the content unchanged.
	UpdateTask(ctx context.Context, id int64, content, status string) (Task, error)

	// DeleteTask removes the task with the given ID. Returns an error
	// if the task does not exist.
	DeleteTask(ctx context.Context, id int64) error

	// ClearTasks removes all tasks from the store.
	ClearTasks(ctx context.Context) error
}

// TaskStore defines the interface for task management.
type TaskStore interface {
	TaskReader
	TaskWriter
}

// Initializer defines an interface for components requiring lifecycle initialization.
type Initializer interface {
	// Initialize performs one-time setup (e.g., creating database tables,
	// running migrations). It must be called before any other methods.
	// Implementations must be idempotent.
	Initialize(ctx context.Context) error
}

// PersistenceProvider provides access to domain-specific persistence services.
type PersistenceProvider interface {
	// GetTasks returns the task management store.
	GetTasks() TaskStore

	// GetSettings returns the key-value store for application settings.
	GetSettings() KVStore

	// GetHealthChecker returns a health checker for the persistence layer.
	GetHealthChecker() HealthChecker
}

// SessionStateProvider manages session-level metadata and state.
type SessionStateProvider interface {
	// GetInfo returns a snapshot of the current session metadata.
	GetInfo() SessionInfo

	// SetInfo replaces the session metadata. The update is immediate
	// and visible to subsequent GetInfo calls.
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
