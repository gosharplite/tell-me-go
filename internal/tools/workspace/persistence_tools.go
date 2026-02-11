// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
)

// PersistenceTools provides tool wrappers for persistence services.
type PersistenceTools struct {
	tasks      *services.TaskService
	scratchpad *services.ScratchpadService
	config     *services.ConfigService
	state      *persistence.SessionState
}

// NewPersistenceTools creates a new PersistenceTools instance.
func NewPersistenceTools(state *persistence.SessionState) *PersistenceTools {
	return &PersistenceTools{
		tasks:      state.Tasks,
		scratchpad: state.Scratchpad,
		config:     state.Config,
		state:      state,
	}
}

// GetSessionInfo handles the get_session_info tool.
func (t *PersistenceTools) GetSessionInfo(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	// Refresh config in session info
	t.state.Info.Config = t.config.GetAll()

	data, err := json.MarshalIndent(t.state.Info, "", "  ")
	if err != nil {
		return tools.ToolResult{}, err
	}
	return tools.ToolResult{Text: string(data)}, nil
}

// Register adds persistence tools to the registry.
func (t *PersistenceTools) Register(r *registry.Registry) {
	r.Register(&tools.ToolDeclaration{
		Name:        "get_session_info",
		Description: "Returns the active configuration, environment variables, and session file paths.",
	}, t.GetSessionInfo)

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "manage_scratchpad",
		Description: "Read, write, or update the persistent scratchpad (scoped to current mode).",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"action": {
					Type:        "STRING",
					Description: "The operation to perform: 'read', 'write' (overwrite), 'append', or 'clear'.",
					Enum:        []string{"read", "write", "append", "clear"},
				},
				"content": {
					Type:        "STRING",
					Description: "The text content to write or append. Required for 'write' and 'append' actions.",
				},
			},
			Required: []string{"action"},
		},
	}, t.ManageScratchpad, registry.ToolOptions{Serial: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "manage_config",
		Description: "Manages persistent key-value configuration/settings scoped by mode.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"action": {
					Type:        "STRING",
					Description: "The operation to perform: 'set', 'get', 'list', 'delete'.",
					Enum:        []string{"set", "get", "list", "delete"},
				},
				"key": {
					Type:        "STRING",
					Description: "The configuration key (e.g., 'teams_webhook_url').",
				},
				"value": {
					Type:        "STRING",
					Description: "The value to store (required for 'set').",
				},
			},
			Required: []string{"action"},
		},
	}, t.ManageConfig, registry.ToolOptions{Serial: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
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
			},
			Required: []string{"action"},
		},
	}, t.ManageTasks, registry.ToolOptions{Serial: true})
}

// ManageTasks handles the manage_tasks tool.
func (t *PersistenceTools) ManageTasks(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Action  string  `json:"action"`
		Content string  `json:"content"`
		Status  string  `json:"status"`
		TaskID  float64 `json:"task_id"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	switch params.Action {
	case "add":
		task, err := t.tasks.AddTask(ctx, params.Content)
		if err != nil {
			return tools.ToolResult{}, err
		}
		return tools.ToolResult{Text: fmt.Sprintf("Task added with ID %.0f", task.ID)}, nil
	case "update":
		if _, err := t.tasks.UpdateTask(ctx, params.TaskID, params.Content, params.Status); err != nil {
			return tools.ToolResult{}, err
		}
		return tools.ToolResult{Text: fmt.Sprintf("Task %.0f updated", params.TaskID)}, nil
	case "delete":
		if err := t.tasks.DeleteTask(ctx, params.TaskID); err != nil {
			return tools.ToolResult{}, err
		}
		return tools.ToolResult{Text: fmt.Sprintf("Task %.0f deleted", params.TaskID)}, nil
	case "list":
		tasks := t.tasks.ListTasks(params.Status)
		if len(tasks) == 0 {
			return tools.ToolResult{Text: "No tasks found."}, nil
		}
		var sb strings.Builder
		sb.WriteString("Tasks:\n")
		for _, task := range tasks {
			icon := "[ ]"
			if task.Status == "completed" {
				icon = "[x]"
			}
			sb.WriteString(fmt.Sprintf("%.0f. %s %s (%s)\n", task.ID, icon, task.Content, task.Status))
		}
		return tools.ToolResult{Text: sb.String()}, nil
	case "clear":
		if err := t.tasks.ClearTasks(ctx); err != nil {
			return tools.ToolResult{}, err
		}
		return tools.ToolResult{Text: "All tasks cleared."}, nil
	default:
		return tools.ToolResult{}, fmt.Errorf("unknown action: %s", params.Action)
	}
}

// ManageScratchpad handles the manage_scratchpad tool.
func (t *PersistenceTools) ManageScratchpad(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Action  string `json:"action"`
		Content string `json:"content"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	switch params.Action {
	case "read":
		content := t.scratchpad.Read()
		if content == "" {
			return tools.ToolResult{Text: "(Scratchpad is empty)"}, nil
		}
		return tools.ToolResult{Text: content}, nil
	case "write":
		if err := t.scratchpad.Write(ctx, params.Content); err != nil {
			return tools.ToolResult{}, err
		}
		return tools.ToolResult{Text: "Scratchpad updated."}, nil
	case "append":
		if err := t.scratchpad.Append(ctx, params.Content); err != nil {
			return tools.ToolResult{}, err
		}
		return tools.ToolResult{Text: "Content appended to scratchpad."}, nil
	case "clear":
		if err := t.scratchpad.Clear(ctx); err != nil {
			return tools.ToolResult{}, err
		}
		return tools.ToolResult{Text: "Scratchpad cleared."}, nil
	default:
		return tools.ToolResult{}, fmt.Errorf("unknown action: %s", params.Action)
	}
}

// ManageConfig handles the manage_config tool.
func (t *PersistenceTools) ManageConfig(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Action string `json:"action"`
		Key    string `json:"key"`
		Value  string `json:"value"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	switch params.Action {
	case "set":
		if err := t.config.Set(ctx, params.Key, params.Value); err != nil {
			return tools.ToolResult{}, err
		}
		return tools.ToolResult{Text: fmt.Sprintf("Config set: %s = %s", params.Key, params.Value)}, nil
	case "get":
		val, err := t.config.Get(params.Key)
		if err != nil {
			return tools.ToolResult{}, err
		}
		return tools.ToolResult{Text: val}, nil
	case "delete":
		if err := t.config.Delete(ctx, params.Key); err != nil {
			return tools.ToolResult{}, err
		}
		return tools.ToolResult{Text: fmt.Sprintf("Config deleted: %s", params.Key)}, nil
	case "list":
		config := t.config.GetAll()
		if len(config) == 0 {
			return tools.ToolResult{Text: "Configuration is empty."}, nil
		}
		var sb strings.Builder
		for k, v := range config {
			sb.WriteString(fmt.Sprintf("%s = %s\n", k, v))
		}
		return tools.ToolResult{Text: sb.String()}, nil
	default:
		return tools.ToolResult{}, fmt.Errorf("unknown action: %s", params.Action)
	}
}
