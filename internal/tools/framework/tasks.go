// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
)

// Task represents a unit of work in the to-do list.
type Task struct {
	ID        float64   `json:"id"`
	Content   string    `json:"content"`
	Status    string    `json:"status"` // pending, completed
	CreatedAt time.Time `json:"created_at"`
}

// TaskStore manages a list of tasks with persistence.
type TaskStore struct {
	mu       sync.RWMutex
	tasks    map[float64]Task
	nextID   float64
	filePath string
	fs       storage.FileSystem
}

// NewTaskStore creates a new TaskStore.
func NewTaskStore(fs storage.FileSystem, filePath string) *TaskStore {
	return &TaskStore{
		tasks:    make(map[float64]Task),
		nextID:   1,
		filePath: filePath,
		fs:       fs,
	}
}

// Load loads tasks from disk.
func (s *TaskStore) Load(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.fs.Stat(ctx, s.filePath); err != nil {
		return nil // File doesn't exist yet, which is fine
	}

	data, err := s.fs.ReadFile(ctx, s.filePath)
	if err != nil {
		return err
	}

	var loaded []Task
	if err := json.Unmarshal(data, &loaded); err != nil {
		return err
	}

	for _, t := range loaded {
		s.tasks[t.ID] = t
		if t.ID >= s.nextID {
			s.nextID = t.ID + 1
		}
	}
	return nil
}

func (s *TaskStore) saveLocked(ctx context.Context, tasks []Task) error {
	// Sort by ID stable
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].ID < tasks[j].ID
	})

	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return err
	}
	return s.fs.WriteFile(ctx, s.filePath, data, 0644)
}

// ManageTasks handles the manage_tasks tool.
func (s *TaskStore) ManageTasks(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	action, _ := args["action"].(string)
	content, _ := args["content"].(string)
	status, _ := args["status"].(string)

	var taskID float64 = -1
	if v, ok := args["task_id"].(float64); ok {
		taskID = v
	}

	switch action {
	case "add":
		return s.add(ctx, content)
	case "update":
		return s.update(ctx, taskID, content, status)
	case "delete":
		return s.delete(ctx, taskID)
	case "list":
		return s.list(status)
	case "clear":
		return s.clear(ctx)
	default:
		return tools.ToolResult{}, fmt.Errorf("unknown action: %s", action)
	}
}

func (s *TaskStore) add(ctx context.Context, content string) (tools.ToolResult, error) {
	if content == "" {
		return tools.ToolResult{}, fmt.Errorf("content is required for add")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	t := Task{
		ID:        s.nextID,
		Content:   content,
		Status:    "pending",
		CreatedAt: time.Now(),
	}

	// Prepare next state
	nextTasks := make([]Task, 0, len(s.tasks)+1)
	for _, task := range s.tasks {
		nextTasks = append(nextTasks, task)
	}
	nextTasks = append(nextTasks, t)

	if err := s.saveLocked(ctx, nextTasks); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to save tasks: %w", err)
	}

	// Commit
	s.tasks[t.ID] = t
	s.nextID++
	return tools.ToolResult{Text: fmt.Sprintf("Task added with ID %.0f", t.ID)}, nil
}

func (s *TaskStore) update(ctx context.Context, taskID float64, content, status string) (tools.ToolResult, error) {
	if taskID == -1 {
		return tools.ToolResult{}, fmt.Errorf("task_id is required for update")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[taskID]
	if !ok {
		return tools.ToolResult{}, fmt.Errorf("task not found: %.0f", taskID)
	}

	// Prepare updated task
	updatedTask := t
	if content != "" {
		updatedTask.Content = content
	}
	if status != "" {
		updatedTask.Status = status
	}

	// Prepare next state
	nextTasks := make([]Task, 0, len(s.tasks))
	for id, task := range s.tasks {
		if id == taskID {
			nextTasks = append(nextTasks, updatedTask)
		} else {
			nextTasks = append(nextTasks, task)
		}
	}

	if err := s.saveLocked(ctx, nextTasks); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to save tasks: %w", err)
	}

	// Commit
	s.tasks[taskID] = updatedTask
	return tools.ToolResult{Text: fmt.Sprintf("Task %.0f updated", taskID)}, nil
}

func (s *TaskStore) delete(ctx context.Context, taskID float64) (tools.ToolResult, error) {
	if taskID == -1 {
		return tools.ToolResult{}, fmt.Errorf("task_id is required for delete")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.tasks[taskID]; !ok {
		return tools.ToolResult{}, fmt.Errorf("task not found: %.0f", taskID)
	}

	// Prepare next state
	nextTasks := make([]Task, 0, len(s.tasks)-1)
	for id, task := range s.tasks {
		if id != taskID {
			nextTasks = append(nextTasks, task)
		}
	}

	if err := s.saveLocked(ctx, nextTasks); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to save tasks: %w", err)
	}

	// Commit
	delete(s.tasks, taskID)
	return tools.ToolResult{Text: fmt.Sprintf("Task %.0f deleted", taskID)}, nil
}

func (s *TaskStore) list(status string) (tools.ToolResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var list []Task
	for _, t := range s.tasks {
		if status != "" && t.Status != status {
			continue
		}
		list = append(list, t)
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].ID < list[j].ID
	})

	if len(list) == 0 {
		return tools.ToolResult{Text: "No tasks found."}, nil
	}

	var sb strings.Builder
	sb.WriteString("Tasks:\n")
	for _, t := range list {
		icon := "[ ]"
		if t.Status == "completed" {
			icon = "[x]"
		}
		sb.WriteString(fmt.Sprintf("%.0f. %s %s (%s)\n", t.ID, icon, t.Content, t.Status))
	}
	return tools.ToolResult{Text: sb.String()}, nil
}

func (s *TaskStore) clear(ctx context.Context) (tools.ToolResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.saveLocked(ctx, []Task{}); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to save tasks: %w", err)
	}

	// Commit
	s.tasks = make(map[float64]Task)
	return tools.ToolResult{Text: "All tasks cleared."}, nil
}
