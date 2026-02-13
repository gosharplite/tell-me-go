// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package services

import (
	"time"
)

// Task represents a unit of work in the to-do list.
type Task struct {
	ID        float64   `json:"id"`
	Content   string    `json:"content"`
	Status    string    `json:"status"` // pending, completed
	CreatedAt time.Time `json:"created_at"`
}

// ISessionProvider provides access to persistence services and session info.
type ISessionProvider interface {
	GetTasks() *TaskService
	GetConfig() *ConfigService
	GetScratchpad() *ScratchpadService
	GetInfo() any
}
