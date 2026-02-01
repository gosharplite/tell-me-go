// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/fsutil"
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
	mu         sync.RWMutex
	tasks      map[float64]Task
	nextID     float64
	filePath   string
}

// NewTaskStore creates a new TaskStore.
func NewTaskStore(filePath string) *TaskStore {
	return &TaskStore{
		tasks:    make(map[float64]Task),
		nextID:   1,
		filePath: filePath,
	}
}

// Load loads tasks from disk.
func (s *TaskStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := os.Stat(s.filePath); err != nil {
		return nil // File doesn't exist yet, which is fine
	}

	data, err := os.ReadFile(s.filePath)
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

// Save saves tasks to disk.
func (s *TaskStore) Save(ctx context.Context) error {
	s.mu.RLock()
	var tasks []Task
	for _, t := range s.tasks {
		tasks = append(tasks, t)
	}
	s.mu.RUnlock()

	// Sort by ID stable
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].ID < tasks[j].ID
	})

	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.AtomicWrite(ctx, s.filePath, data, 0644)
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
	t := Task{
		ID:        s.nextID,
		Content:   content,
		Status:    "pending",
		CreatedAt: time.Now(),
	}
	s.tasks[s.nextID] = t
	s.nextID++
	s.mu.Unlock()

	if err := s.Save(ctx); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to save tasks: %w", err)
	}
	return tools.ToolResult{Text: fmt.Sprintf("Task added with ID %.0f", t.ID)}, nil
}

func (s *TaskStore) update(ctx context.Context, taskID float64, content, status string) (tools.ToolResult, error) {
	if taskID == -1 {
		return tools.ToolResult{}, fmt.Errorf("task_id is required for update")
	}

	s.mu.Lock()
	t, ok := s.tasks[taskID]
	if !ok {
		s.mu.Unlock()
		return tools.ToolResult{}, fmt.Errorf("task not found: %.0f", taskID)
	}
	if content != "" {
		t.Content = content
	}
	if status != "" {
		t.Status = status
	}
	s.tasks[taskID] = t
	s.mu.Unlock()

	if err := s.Save(ctx); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to save tasks: %w", err)
	}
	return tools.ToolResult{Text: fmt.Sprintf("Task %.0f updated", taskID)}, nil
}

func (s *TaskStore) delete(ctx context.Context, taskID float64) (tools.ToolResult, error) {
	if taskID == -1 {
		return tools.ToolResult{}, fmt.Errorf("task_id is required for delete")
	}

	s.mu.Lock()
	if _, ok := s.tasks[taskID]; !ok {
		s.mu.Unlock()
		return tools.ToolResult{}, fmt.Errorf("task not found: %.0f", taskID)
	}
	delete(s.tasks, taskID)
	s.mu.Unlock()

	if err := s.Save(ctx); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to save tasks: %w", err)
	}
	return tools.ToolResult{Text: fmt.Sprintf("Task %.0f deleted", taskID)}, nil
}

func (s *TaskStore) list(status string) (tools.ToolResult, error) {
	s.mu.RLock()
	var list []Task
	for _, t := range s.tasks {
		if status != "" && t.Status != status {
			continue
		}
		list = append(list, t)
	}
	s.mu.RUnlock()

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
	s.tasks = make(map[float64]Task)
	s.mu.Unlock()

	if err := s.Save(ctx); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to save tasks: %w", err)
	}
	return tools.ToolResult{Text: "All tasks cleared."}, nil
}
