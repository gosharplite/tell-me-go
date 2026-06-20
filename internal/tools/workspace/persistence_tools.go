// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// persistenceTools provides tool wrappers for persistence services.
type persistenceTools struct {
	tasks         ports.TaskStore
	state         ports.SessionProvider
	reg           tools.ToolMetadataProvider
	marshalIndent func(v any, prefix, indent string) ([]byte, error)
}

// newpersistenceTools creates a new persistenceTools instance.
func newpersistenceTools(state ports.SessionProvider, reg tools.ToolMetadataProvider) *persistenceTools {
	if state == nil {
		return &persistenceTools{
			marshalIndent: json.MarshalIndent,
		}
	}

	// Handle interface-nil-pointer trap
	v := reflect.ValueOf(state)
	if v.Kind() == reflect.Pointer && v.IsNil() {
		return &persistenceTools{
			marshalIndent: json.MarshalIndent,
		}
	}

	return &persistenceTools{
		tasks:         state.GetTasks(),
		state:         state,
		reg:           reg,
		marshalIndent: json.MarshalIndent,
	}
}

// retryOnBusy retries the given operation up to 3 times with exponential
// backoff (100ms → 200ms → 400ms) when the error contains "database is locked"
// (SQLITE_BUSY). All other errors are returned immediately. Respects context
// cancellation between retry attempts.
func retryOnBusy(ctx context.Context, op func() error) error {
	delay := 100 * time.Millisecond
	for attempt := 0; attempt < 3; attempt++ {
		err := op()
		if err == nil {
			return nil
		}
		if !strings.Contains(err.Error(), "database is locked") {
			return err
		}
		if attempt == 2 {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
			delay *= 2
		}
	}
	return nil // unreachable
}

// GetSessionInfo handles the get_session_info tool.
func (t *persistenceTools) GetSessionInfo(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	info := t.state.GetInfo()

	data, err := t.marshalIndent(info, "", "  ")
	if err != nil {
		return tools.ToolResult{}, err
	}
	return tools.ToolResult{Text: string(data)}, nil
}

// Register adds persistence tools to the registrar.
func (t *persistenceTools) Register(r tools.ToolRegistrar) error {
	if t.state == nil {
		return nil
	}

	if err := r.Register(&tools.ToolDeclaration{
		Name:        "get_session_info",
		Description: "Returns the active configuration, environment variables, and session file paths.",
	}, t.GetSessionInfo); err != nil {
		return err
	}

	if err := r.RegisterWithOptions(loadToolkitDef, t.handleLoadToolkit, tools.ToolOptions{Serial: true}); err != nil {
		return err
	}

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "manage_tasks",
		Description: "Manages a to-do list of tasks (scoped to current mode).",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"action": {
					Type:        "STRING",
					Description: "The action to perform: 'add', 'update', 'list', 'delete', 'clear'.",
					Enum:        []string{"add", "update", "list", "delete", "clear"},
				},
				"task_id": {
					Type:        "INTEGER",
					Description: "The ID of the task to update or delete.",
				},
				"content": {
					Type:        "STRING",
					Description: "The task description (required for 'add').",
				},
				"status": {
					Type:        "STRING",
					Description: "The new status (e.g., 'completed', 'pending') for 'update' or filter for 'list'.",
				},
				"limit": {
					Type:        "INTEGER",
					Description: "Maximum tasks to return for the 'list' action. Default: 50. Use 0 for unlimited.",
				},
				"offset": {
					Type:        "INTEGER",
					Description: "Number of tasks to skip for the 'list' action. Default: 0. Use with limit for pagination.",
				},
			},
			Required: []string{"action"},
		},
	}, t.ManageTasks, tools.ToolOptions{Serial: false}); err != nil {
		return err
	}
	return nil
}

// ManageTasks handles the manage_tasks tool.
func (t *persistenceTools) ManageTasks(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Action  string `json:"action"`
		Content string `json:"content"`
		Status  string `json:"status"`
		TaskID  int64  `json:"task_id"`
		Limit   int    `json:"limit"`
		Offset  int    `json:"offset"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	switch params.Action {
	case "add":
		return t.addTask(ctx, params.Content)
	case "update":
		return t.updateTask(ctx, params.TaskID, params.Content, params.Status)
	case "delete":
		return t.deleteTask(ctx, params.TaskID)
	case "list":
		return t.listTasks(ctx, params.Status, params.Limit, params.Offset)
	case "clear":
		return t.clearTasks(ctx)
	default:
		return tools.ToolResult{Text: fmt.Sprintf("Error: unknown action: %s", params.Action)}, nil
	}
}

func (t *persistenceTools) addTask(ctx context.Context, content string) (tools.ToolResult, error) {
	var task ports.Task
	err := retryOnBusy(ctx, func() error {
		var e error
		task, e = t.tasks.AddTask(ctx, content)
		return e
	})
	if err != nil {
		return tools.ToolResult{}, err
	}
	return tools.ToolResult{Text: fmt.Sprintf("Task added with ID %d", task.ID)}, nil
}

func (t *persistenceTools) updateTask(ctx context.Context, id int64, content, status string) (tools.ToolResult, error) {
	err := retryOnBusy(ctx, func() error {
		_, e := t.tasks.UpdateTask(ctx, id, content, status)
		return e
	})
	if err != nil {
		return tools.ToolResult{}, err
	}
	return tools.ToolResult{Text: fmt.Sprintf("Task %d updated", id)}, nil
}

func (t *persistenceTools) deleteTask(ctx context.Context, id int64) (tools.ToolResult, error) {
	err := retryOnBusy(ctx, func() error {
		return t.tasks.DeleteTask(ctx, id)
	})
	if err != nil {
		return tools.ToolResult{}, err
	}
	return tools.ToolResult{Text: fmt.Sprintf("Task %d deleted", id)}, nil
}

// fetchAndCount retrieves tasks and total count in a single call site.
// This consolidates two sequential I/O operations so callers don't repeat
// the paired ListTasks+CountTasks pattern.
func (t *persistenceTools) fetchAndCount(ctx context.Context, status string, limit, offset int) ([]ports.Task, int, error) {
	tasks, err := t.tasks.ListTasks(ctx, status, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	count, err := t.tasks.CountTasks(ctx, status)
	if err != nil {
		return nil, 0, err
	}
	return tasks, count, nil
}

func (t *persistenceTools) listTasks(ctx context.Context, status string, limit, offset int) (tools.ToolResult, error) {
	if limit == 0 {
		limit = 50
	}
	tasks, totalCount, err := t.fetchAndCount(ctx, status, limit, offset)
	if err != nil {
		return tools.ToolResult{}, err
	}
	return tools.ToolResult{Text: renderTaskPage(tasks, totalCount, offset, limit)}, nil
}

func (t *persistenceTools) clearTasks(ctx context.Context) (tools.ToolResult, error) {
	err := retryOnBusy(ctx, func() error {
		return t.tasks.ClearTasks(ctx)
	})
	if err != nil {
		return tools.ToolResult{}, err
	}
	return tools.ToolResult{Text: "All tasks cleared."}, nil
}

// taskIcon returns the checkbox icon for a task's status.
func taskIcon(task ports.Task) string {
	if task.Status == "completed" {
		return "[x]"
	}
	return "[ ]"
}

// renderTaskPage formats a task listing into a human-readable string.
// It is a pure function with no external dependencies.
func renderTaskPage(tasks []ports.Task, totalCount, offset, limit int) string {
	if len(tasks) == 0 {
		if totalCount > 0 {
			return fmt.Sprintf("No tasks found. (total: %d)", totalCount)
		}
		return "No tasks found."
	}

	var sb strings.Builder
	from := offset + 1
	to := offset + len(tasks)
	_, _ = fmt.Fprintf(&sb, "Tasks (showing %d-%d of %d):\n", from, to, totalCount)

	for _, task := range tasks {
		_, _ = fmt.Fprintf(&sb, "%d. %s %s (%s)\n", task.ID, taskIcon(task), task.Content, task.Status)
	}

	if len(tasks) == limit && (offset+limit) < totalCount {
		_, _ = fmt.Fprintf(&sb, "\nUse offset=%d for next page.", offset+limit)
	}

	return sb.String()
}
