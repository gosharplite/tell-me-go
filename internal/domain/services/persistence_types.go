// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package services

import (
	"context"
	"time"
)

// Task represents a unit of work in the to-do list.
type Task struct {
	ID        float64   `json:"id"`
	Content   string    `json:"content"`
	Status    string    `json:"status"` // pending, completed
	CreatedAt time.Time `json:"created_at"`
}

// TaskRepository defines the interface for persisting tasks.
type TaskRepository interface {
	LoadTasks(ctx context.Context) ([]Task, error)
	SaveTasks(ctx context.Context, tasks []Task) error
}

// ScratchpadRepository defines the interface for persisting the scratchpad.
type ScratchpadRepository interface {
	LoadScratchpad(ctx context.Context) (string, error)
	SaveScratchpad(ctx context.Context, content string) error
}

// ConfigRepository defines the interface for persisting configuration.
type ConfigRepository interface {
	LoadConfig(ctx context.Context) (map[string]string, error)
	SaveConfig(ctx context.Context, config map[string]string) error
}
