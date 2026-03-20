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
	tasks      ports.TaskService
	scratchpad ports.IScratchpadService
	config     ports.IConfigService
	state      ports.ISessionProvider
}

// newpersistenceTools creates a new persistenceTools instance.
func newpersistenceTools(state ports.ISessionProvider) *persistenceTools {
	if state == nil {
		return &persistenceTools{}
	}

	// Handle interface-nil-pointer trap
	v := reflect.ValueOf(state)
	if v.Kind() == reflect.Ptr && v.IsNil() {
		return &persistenceTools{}
	}

	return &persistenceTools{
		tasks:      state.GetTasks(),
		scratchpad: state.GetScratchpad(),
		config:     state.GetConfig(),
		state:      state,
	}
}

// GetSessionInfo handles the get_session_info tool.
func (t *persistenceTools) GetSessionInfo(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
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

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
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
	}, t.ManageScratchpad, tools.ToolOptions{Serial: false}); err != nil {
		return err
	}

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
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
	}, t.ManageConfig, tools.ToolOptions{Serial: false}); err != nil {
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
			},
			Required: []string{"action"},
		},
	}, t.ManageTasks, tools.ToolOptions{Serial: false}); err != nil {
		return err
	}
	return nil
}

// ManageTasks handles the manage_tasks tool.
func (t *persistenceTools) ManageTasks(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
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
		return t.addTask(ctx, params.Content)
	case "update":
		return t.updateTask(ctx, params.TaskID, params.Content, params.Status)
	case "delete":
		return t.deleteTask(ctx, params.TaskID)
	case "list":
		return t.listTasks(params.Status)
	case "clear":
		return t.clearTasks(ctx)
	default:
		return tools.ToolResult{}, fmt.Errorf("unknown action: %s", params.Action)
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

func (t *persistenceTools) listTasks(status string) (tools.ToolResult, error) {
	tasks := t.tasks.ListTasks(status)
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
		_, _ = fmt.Fprintf(&sb, "%.0f. %s %s (%s)\n", task.ID, icon, task.Content, task.Status)
	}
	return tools.ToolResult{Text: sb.String()}, nil
}

func (t *persistenceTools) clearTasks(ctx context.Context) (tools.ToolResult, error) {
	if err := t.tasks.ClearTasks(ctx); err != nil {
		return tools.ToolResult{}, err
	}
	return tools.ToolResult{Text: "All tasks cleared."}, nil
}

// ManageScratchpad handles the manage_scratchpad tool.
func (t *persistenceTools) ManageScratchpad(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Action  string `json:"action"`
		Content string `json:"content"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	switch params.Action {
	case "read":
		return t.readScratchpad()
	case "write":
		return t.writeScratchpad(ctx, params.Content)
	case "append":
		return t.appendScratchpad(ctx, params.Content)
	case "clear":
		return t.clearScratchpad(ctx)
	default:
		return tools.ToolResult{}, fmt.Errorf("unknown action: %s", params.Action)
	}
}

func (t *persistenceTools) readScratchpad() (tools.ToolResult, error) {
	content := t.scratchpad.Read()
	if content == "" {
		return tools.ToolResult{Text: "(Scratchpad is empty)"}, nil
	}
	return tools.ToolResult{Text: content}, nil
}

func (t *persistenceTools) writeScratchpad(ctx context.Context, content string) (tools.ToolResult, error) {
	if err := t.scratchpad.Write(ctx, content); err != nil {
		return tools.ToolResult{}, err
	}
	return tools.ToolResult{Text: "Scratchpad updated."}, nil
}

func (t *persistenceTools) appendScratchpad(ctx context.Context, content string) (tools.ToolResult, error) {
	if err := t.scratchpad.Append(ctx, content); err != nil {
		return tools.ToolResult{}, err
	}
	return tools.ToolResult{Text: "Content appended to scratchpad."}, nil
}

func (t *persistenceTools) clearScratchpad(ctx context.Context) (tools.ToolResult, error) {
	if err := t.scratchpad.Clear(ctx); err != nil {
		return tools.ToolResult{}, err
	}
	return tools.ToolResult{Text: "Scratchpad cleared."}, nil
}

// ManageConfig handles the manage_config tool.
func (t *persistenceTools) ManageConfig(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
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
		return t.setConfig(ctx, params.Key, params.Value)
	case "get":
		return t.getConfig(params.Key)
	case "delete":
		return t.deleteConfig(ctx, params.Key)
	case "list":
		return t.listConfig()
	default:
		return tools.ToolResult{}, fmt.Errorf("unknown action: %s", params.Action)
	}
}

func (t *persistenceTools) setConfig(ctx context.Context, key, value string) (tools.ToolResult, error) {
	if err := t.config.Set(ctx, key, value); err != nil {
		return tools.ToolResult{}, err
	}
	return tools.ToolResult{Text: fmt.Sprintf("Config set: %s = %s", key, value)}, nil
}

func (t *persistenceTools) getConfig(key string) (tools.ToolResult, error) {
	val, err := t.config.Get(key)
	if err != nil {
		return tools.ToolResult{}, err
	}
	return tools.ToolResult{Text: val}, nil
}

func (t *persistenceTools) deleteConfig(ctx context.Context, key string) (tools.ToolResult, error) {
	if err := t.config.Delete(ctx, key); err != nil {
		return tools.ToolResult{}, err
	}
	return tools.ToolResult{Text: fmt.Sprintf("Config deleted: %s", key)}, nil
}

func (t *persistenceTools) listConfig() (tools.ToolResult, error) {
	config := t.config.GetAll()
	if len(config) == 0 {
		return tools.ToolResult{Text: "Configuration is empty."}, nil
	}
	var sb strings.Builder
	for k, v := range config {
		_, _ = fmt.Fprintf(&sb, "%s = %s\n", k, v)
	}
	return tools.ToolResult{Text: sb.String()}, nil
}
