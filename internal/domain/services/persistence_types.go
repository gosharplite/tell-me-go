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

// SessionInfo holds metadata about the current execution environment.
type SessionInfo struct {
	Config map[string]string `json:"config"`
	Env    map[string]string `json:"env"`
	Paths  map[string]string `json:"paths"`
}

// ISessionProvider provides access to persistence services and session info.
type ISessionProvider interface {
	GetTasks() *TaskService
	GetConfig() *ConfigService
	GetScratchpad() *ScratchpadService
	GetInfo() SessionInfo
}
