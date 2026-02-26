// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import (
	"context"
	"time"
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

// ITaskService defines the interface for task management.
type ITaskService interface {
	Initialize(ctx context.Context) error
	AddTask(ctx context.Context, content string) (Task, error)
	UpdateTask(ctx context.Context, id float64, content, status string) (Task, error)
	DeleteTask(ctx context.Context, id float64) error
	ListTasks(status string) []Task
	ClearTasks(ctx context.Context) error
}

// IConfigService defines the interface for configuration management.
type IConfigService interface {
	Initialize(ctx context.Context) error
	Set(ctx context.Context, key, val string) error
	Get(key string) (string, error)
	Delete(ctx context.Context, key string) error
	GetAll() map[string]string
}

// IScratchpadService defines the interface for scratchpad management.
type IScratchpadService interface {
	Initialize(ctx context.Context) error
	Read() string
	Write(ctx context.Context, content string) error
	Append(ctx context.Context, content string) error
	Clear(ctx context.Context) error
}

// ISessionProvider provides access to persistence services and session info.
type ISessionProvider interface {
	GetTasks() ITaskService
	GetConfig() IConfigService
	GetScratchpad() IScratchpadService
	GetInfo() SessionInfo
	SetInfo(info SessionInfo)
	Close() error
}
