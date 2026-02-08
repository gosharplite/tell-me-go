// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

type stateManager struct {
	sm          *security.SecurityManager
	tasks       *TaskStore
	config      *ConfigStore
	scratchpad  *ScratchpadStore
	sessionInfo SessionInfo
}

// SessionInfo holds metadata about the current execution environment.
type SessionInfo struct {
	Config map[string]string `json:"config"`
	Env    map[string]string `json:"env"`
	Paths  map[string]string `json:"paths"`
}

// RegisterState adds state management tools (scratchpad, config, tasks) to the registry.
func RegisterState(r *registry.Registry, sm *security.SecurityManager, configDir string) {
	fs := fsutil.DefaultFileSystem
	m := &stateManager{
		sm:         sm,
		tasks:      NewTaskStore(fs, filepath.Join(configDir, "tasks.json")),
		config:     NewConfigStore(fs, filepath.Join(configDir, "config.json")),
		scratchpad: NewScratchpadStore(fs, filepath.Join(configDir, "scratchpad.md")),
	}

	// Initialize state
	ctx := context.Background()
	_ = m.config.Load(ctx)
	_ = m.scratchpad.Load(ctx)
	_ = m.tasks.Load(ctx)
	m.initSessionInfo(configDir)

	r.Register(&tools.ToolDeclaration{
		Name:        "get_session_info",
		Description: "Returns the active configuration, environment variables, and session file paths.",
	}, m.getSessionInfo)

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
	}, m.scratchpad.ManageScratchpad, registry.ToolOptions{Serial: true})

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
	}, m.config.ManageConfig, registry.ToolOptions{Serial: true})

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
	}, m.tasks.ManageTasks, registry.ToolOptions{Serial: true})
}

func (m *stateManager) initSessionInfo(configDir string) {
	m.sessionInfo = SessionInfo{
		Config: m.config.GetAll(),
		Env: map[string]string{
			"TELL_ME_MODE": os.Getenv("TELL_ME_MODE"),
		},
		Paths: map[string]string{
			"config_dir":   configDir,
			"scratch_file": m.scratchpad.filePath,
			"tasks_file":   m.tasks.filePath,
			"config_file":  m.config.filePath,
		},
	}
}

func (m *stateManager) getSessionInfo(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	// Refresh config in session info
	m.sessionInfo.Config = m.config.GetAll()

	data, err := json.MarshalIndent(m.sessionInfo, "", "  ")
	if err != nil {
		return tools.ToolResult{}, err
	}
	return tools.ToolResult{Text: string(data)}, nil
}
