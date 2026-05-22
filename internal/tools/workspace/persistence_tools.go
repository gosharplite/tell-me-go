// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// persistenceTools provides tool wrappers for persistence services.
type persistenceTools struct {
	tasks ports.TaskStore
	state ports.SessionProvider
	reg   tools.ToolMetadataProvider
}

// newpersistenceTools creates a new persistenceTools instance.
func newpersistenceTools(state ports.SessionProvider, reg tools.ToolMetadataProvider) *persistenceTools {
	if state == nil {
		return &persistenceTools{}
	}

	// Handle interface-nil-pointer trap
	v := reflect.ValueOf(state)
	if v.Kind() == reflect.Pointer && v.IsNil() {
		return &persistenceTools{}
	}

	return &persistenceTools{
		tasks: state.GetTasks(),
		state: state,
		reg:   reg,
	}
}

// GetSessionInfo handles the get_session_info tool.
func (t *persistenceTools) GetSessionInfo(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	info := t.state.GetInfo()

	data, err := json.MarshalIndent(info, "", "  ")
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
					Type:        "NUMBER",
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
					Type:        "NUMBER",
					Description: "Maximum tasks to return for the 'list' action. Default: 50. Use 0 for unlimited.",
				},
				"offset": {
					Type:        "NUMBER",
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
		Action  string  `json:"action"`
		Content string  `json:"content"`
		Status  string  `json:"status"`
		TaskID  float64 `json:"task_id"`
		Limit   float64 `json:"limit"`
		Offset  float64 `json:"offset"`
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
		return t.listTasks(params.Status, int(params.Limit), int(params.Offset))
	case "clear":
		return t.clearTasks(ctx)
	default:
		return tools.ToolResult{Text: fmt.Sprintf("Error: unknown action: %s", params.Action)}, nil
	}
}

func (t *persistenceTools) addTask(ctx context.Context, content string) (tools.ToolResult, error) {
	task, err := t.tasks.AddTask(ctx, content)
	if err != nil {
		return tools.ToolResult{}, err
	}
	return tools.ToolResult{Text: fmt.Sprintf("Task added with ID %.0f", task.ID)}, nil
}

func (t *persistenceTools) updateTask(ctx context.Context, id float64, content, status string) (tools.ToolResult, error) {
	if _, err := t.tasks.UpdateTask(ctx, id, content, status); err != nil {
		return tools.ToolResult{}, err
	}
	return tools.ToolResult{Text: fmt.Sprintf("Task %.0f updated", id)}, nil
}

func (t *persistenceTools) deleteTask(ctx context.Context, id float64) (tools.ToolResult, error) {
	if err := t.tasks.DeleteTask(ctx, id); err != nil {
		return tools.ToolResult{}, err
	}
	return tools.ToolResult{Text: fmt.Sprintf("Task %.0f deleted", id)}, nil
}

func (t *persistenceTools) listTasks(status string, limit, offset int) (tools.ToolResult, error) {
	if limit == 0 {
		limit = 50
	}
	tasks := t.tasks.ListTasks(status, limit, offset)
	totalCount := t.tasks.CountTasks(status)

	if len(tasks) == 0 {
		if totalCount > 0 {
			return tools.ToolResult{Text: fmt.Sprintf("No tasks found. (total: %d)", totalCount)}, nil
		}
		return tools.ToolResult{Text: "No tasks found."}, nil
	}

	var sb strings.Builder
	// Pagination summary header
	from := offset + 1
	to := offset + len(tasks)
	fmt.Fprintf(&sb, "Tasks (showing %d-%d of %d):\n", from, to, totalCount)

	for _, task := range tasks {
		icon := "[ ]"
		if task.Status == "completed" {
			icon = "[x]"
		}
		_, _ = fmt.Fprintf(&sb, "%.0f. %s %s (%s)\n", task.ID, icon, task.Content, task.Status)
	}

	// Pagination hint when there are more pages
	if len(tasks) == limit && (offset+limit) < totalCount {
		fmt.Fprintf(&sb, "\nUse offset=%d for next page.", offset+limit)
	}

	return tools.ToolResult{Text: sb.String()}, nil
}

func (t *persistenceTools) clearTasks(ctx context.Context) (tools.ToolResult, error) {
	if err := t.tasks.ClearTasks(ctx); err != nil {
		return tools.ToolResult{}, err
	}
	return tools.ToolResult{Text: "All tasks cleared."}, nil
}
