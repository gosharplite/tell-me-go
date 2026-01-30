// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"google.golang.org/genai"
)

type stateManager struct {
	sm          *SecurityManager
	mu          sync.RWMutex
	tasks       map[float64]Task
	taskNextID  float64
	config      map[string]string
	configFile  string
	scratchpad  string
	scratchFile string
	tasksFile   string
	sessionInfo SessionInfo
}

// SessionInfo holds metadata about the current execution environment.
type SessionInfo struct {
	Config map[string]string `json:"config"`
	Env    map[string]string `json:"env"`
	Paths  map[string]string `json:"paths"`
}

// Task represents a unit of work in the to-do list.
type Task struct {
	ID        float64   `json:"id"`
	Content   string    `json:"content"`
	Status    string    `json:"status"` // pending, completed
	CreatedAt time.Time `json:"created_at"`
}

// RegisterStateTools adds state management tools (scratchpad, config, tasks) to the registry.
func RegisterStateTools(r *Registry, sm *SecurityManager, configDir string) {
	m := &stateManager{
		sm:          sm,
		tasks:       make(map[float64]Task),
		taskNextID:  1,
		config:      make(map[string]string),
		configFile:  fmt.Sprintf("%s/config.json", configDir),
		scratchFile: fmt.Sprintf("%s/scratchpad.md", configDir),
		tasksFile:   fmt.Sprintf("%s/tasks.json", configDir),
	}

	// Initialize state
	m.loadConfig()
	m.loadScratchpad()
	m.loadTasks()
	m.initSessionInfo(configDir)

	r.Register(&genai.FunctionDeclaration{
		Name:        "get_session_info",
		Description: "Returns the active configuration, environment variables, and session file paths.",
	}, m.getSessionInfo)

	r.Register(&genai.FunctionDeclaration{
		Name:        "manage_scratchpad",
		Description: "Read, write, or update the persistent scratchpad (scoped to current mode).",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"action": {
					Type:        genai.TypeString,
					Description: "The operation to perform: 'read', 'write' (overwrite), 'append', or 'clear'.",
					Enum:        []string{"read", "write", "append", "clear"},
				},
				"content": {
					Type:        genai.TypeString,
					Description: "The text content to write or append. Required for 'write' and 'append' actions.",
				},
			},
			Required: []string{"action"},
		},
	}, m.manageScratchpad)

	r.Register(&genai.FunctionDeclaration{
		Name:        "manage_config",
		Description: "Manages persistent key-value configuration/settings scoped by mode.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"action": {
					Type:        genai.TypeString,
					Description: "The operation to perform: 'set', 'get', 'list', 'delete'.",
					Enum:        []string{"set", "get", "list", "delete"},
				},
				"key": {
					Type:        genai.TypeString,
					Description: "The configuration key (e.g., 'teams_webhook_url').",
				},
				"value": {
					Type:        genai.TypeString,
					Description: "The value to store (required for 'set').",
				},
			},
			Required: []string{"action"},
		},
	}, m.manageConfig)

	r.Register(&genai.FunctionDeclaration{
		Name:        "configure_ux_preferences",
		Description: "Updates the persistent configuration for 'smart_suggestions'. Set to 'on' to enable context-aware follow-up command suggestions at the end of responses, or 'off' to disable them.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"feature": {
					Type:        genai.TypeString,
					Description: "The UX feature to configure.",
					Enum:        []string{"smart_suggestions"},
				},
				"status": {
					Type:        genai.TypeString,
					Description: "Whether the feature is 'on' or 'off'.",
					Enum:        []string{"on", "off"},
				},
			},
			Required: []string{"feature", "status"},
		},
	}, m.configureUXPreferences)

	r.Register(&genai.FunctionDeclaration{
		Name:        "manage_tasks",
		Description: "Manages a to-do list of tasks (scoped to current mode).",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"action": {
					Type:        genai.TypeString,
					Description: "The action to perform: 'add', 'update', 'list', 'delete', 'clear'.",
					Enum:        []string{"add", "update", "list", "delete", "clear"},
				},
				"task_id": {
					Type:        genai.TypeNumber,
					Description: "The ID of the task to update or delete.",
				},
				"content": {
					Type:        genai.TypeString,
					Description: "The task description (required for 'add').",
				},
				"status": {
					Type:        genai.TypeString,
					Description: "The new status (e.g., 'completed', 'pending') for 'update' or filter for 'list'.",
				},
			},
			Required: []string{"action"},
		},
	}, m.manageTasks)
}

func (m *stateManager) initSessionInfo(configDir string) {
	m.sessionInfo = SessionInfo{
		Config: m.config,
		Env: map[string]string{
			"TELL_ME_MODE": os.Getenv("TELL_ME_MODE"),
		},
		Paths: map[string]string{
			"config_dir":   configDir,
			"scratch_file": m.scratchFile,
			"tasks_file":   m.tasksFile,
			"config_file":  m.configFile,
		},
	}
}

func (m *stateManager) loadConfig() {
	if _, err := os.Stat(m.configFile); err == nil {
		if data, err := os.ReadFile(m.configFile); err == nil {
			_ = json.Unmarshal(data, &m.config)
		}
	}
}

func (m *stateManager) saveConfig() error {
	data, err := json.MarshalIndent(m.config, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.AtomicWrite(m.configFile, data, 0644)
}

func (m *stateManager) loadScratchpad() {
	if _, err := os.Stat(m.scratchFile); err == nil {
		if data, err := os.ReadFile(m.scratchFile); err == nil {
			m.scratchpad = string(data)
		}
	}
}

func (m *stateManager) saveScratchpad() error {
	return fsutil.AtomicWrite(m.scratchFile, []byte(m.scratchpad), 0644)
}

func (m *stateManager) loadTasks() {
	if _, err := os.Stat(m.tasksFile); err == nil {
		if data, err := os.ReadFile(m.tasksFile); err == nil {
			var loaded []Task
			if err := json.Unmarshal(data, &loaded); err == nil {
				for _, t := range loaded {
					m.tasks[t.ID] = t
					if t.ID >= m.taskNextID {
						m.taskNextID = t.ID + 1
					}
				}
			}
		}
	}
}

func (m *stateManager) saveTasks() error {
	var tasks []Task
	for _, t := range m.tasks {
		tasks = append(tasks, t)
	}
	// Sort by ID stable
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].ID < tasks[j].ID
	})

	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.AtomicWrite(m.tasksFile, data, 0644)
}

func (m *stateManager) getSessionInfo(ctx context.Context, args map[string]interface{}) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Refresh config in session info
	m.sessionInfo.Config = m.config

	data, err := json.MarshalIndent(m.sessionInfo, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *stateManager) manageScratchpad(ctx context.Context, args map[string]interface{}) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	action, _ := args["action"].(string)
	content, _ := args["content"].(string)

	switch action {
	case "read":
		if m.scratchpad == "" {
			return "(Scratchpad is empty)", nil
		}
		return m.scratchpad, nil
	case "write":
		m.scratchpad = content
		if err := m.saveScratchpad(); err != nil {
			return "", fmt.Errorf("failed to save scratchpad: %w", err)
		}
		return "Scratchpad updated.", nil
	case "append":
		if m.scratchpad != "" {
			m.scratchpad += "\n"
		}
		m.scratchpad += content
		if err := m.saveScratchpad(); err != nil {
			return "", fmt.Errorf("failed to save scratchpad: %w", err)
		}
		return "Content appended to scratchpad.", nil
	case "clear":
		m.scratchpad = ""
		if err := m.saveScratchpad(); err != nil {
			return "", fmt.Errorf("failed to save scratchpad: %w", err)
		}
		return "Scratchpad cleared.", nil
	default:
		return "", fmt.Errorf("unknown action: %s", action)
	}
}

func (m *stateManager) manageConfig(ctx context.Context, args map[string]interface{}) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	action, _ := args["action"].(string)
	key, _ := args["key"].(string)
	val, _ := args["value"].(string)

	switch action {
	case "set":
		if key == "" {
			return "", fmt.Errorf("key is required for set")
		}
		m.config[key] = val
		if err := m.saveConfig(); err != nil {
			return "", fmt.Errorf("failed to save config: %w", err)
		}
		return fmt.Sprintf("Config set: %s = %s", key, val), nil
	case "get":
		if key == "" {
			return "", fmt.Errorf("key is required for get")
		}
		if v, ok := m.config[key]; ok {
			return v, nil
		}
		return "", fmt.Errorf("key not found: %s", key)
	case "delete":
		if key == "" {
			return "", fmt.Errorf("key is required for delete")
		}
		delete(m.config, key)
		if err := m.saveConfig(); err != nil {
			return "", fmt.Errorf("failed to save config: %w", err)
		}
		return fmt.Sprintf("Config deleted: %s", key), nil
	case "list":
		var sb strings.Builder
		for k, v := range m.config {
			sb.WriteString(fmt.Sprintf("%s = %s\n", k, v))
		}
		if sb.Len() == 0 {
			return "Configuration is empty.", nil
		}
		return sb.String(), nil
	default:
		return "", fmt.Errorf("unknown action: %s", action)
	}
}

func (m *stateManager) configureUXPreferences(ctx context.Context, args map[string]interface{}) (string, error) {
	feature, _ := args["feature"].(string)
	status, _ := args["status"].(string)

	if feature != "smart_suggestions" {
		return "", fmt.Errorf("unsupported feature: %s", feature)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.config[feature] = status
	if err := m.saveConfig(); err != nil {
		return "", fmt.Errorf("failed to save preference: %w", err)
	}

	return fmt.Sprintf("UX Preference updated: %s = %s", feature, status), nil
}

func (m *stateManager) manageTasks(ctx context.Context, args map[string]interface{}) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	action, _ := args["action"].(string)
	content, _ := args["content"].(string)
	status, _ := args["status"].(string)

	var taskID float64 = -1
	if v, ok := args["task_id"].(float64); ok {
		taskID = v
	}

	switch action {
	case "add":
		if content == "" {
			return "", fmt.Errorf("content is required for add")
		}
		t := Task{
			ID:        m.taskNextID,
			Content:   content,
			Status:    "pending",
			CreatedAt: time.Now(),
		}
		m.tasks[m.taskNextID] = t
		m.taskNextID++
		if err := m.saveTasks(); err != nil {
			return "", fmt.Errorf("failed to save tasks: %w", err)
		}
		return fmt.Sprintf("Task added with ID %.0f", t.ID), nil

	case "update":
		if taskID == -1 {
			return "", fmt.Errorf("task_id is required for update")
		}
		t, ok := m.tasks[taskID]
		if !ok {
			return "", fmt.Errorf("task not found: %.0f", taskID)
		}
		if content != "" {
			t.Content = content
		}
		if status != "" {
			t.Status = status
		}
		m.tasks[taskID] = t
		if err := m.saveTasks(); err != nil {
			return "", fmt.Errorf("failed to save tasks: %w", err)
		}
		return fmt.Sprintf("Task %.0f updated", taskID), nil

	case "delete":
		if taskID == -1 {
			return "", fmt.Errorf("task_id is required for delete")
		}
		if _, ok := m.tasks[taskID]; !ok {
			return "", fmt.Errorf("task not found: %.0f", taskID)
		}
		delete(m.tasks, taskID)
		if err := m.saveTasks(); err != nil {
			return "", fmt.Errorf("failed to save tasks: %w", err)
		}
		return fmt.Sprintf("Task %.0f deleted", taskID), nil

	case "list":
		var list []Task
		for _, t := range m.tasks {
			if status != "" && t.Status != status {
				continue
			}
			list = append(list, t)
		}
		sort.Slice(list, func(i, j int) bool {
			return list[i].ID < list[j].ID
		})

		if len(list) == 0 {
			return "No tasks found.", nil
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
		return sb.String(), nil

	case "clear":
		m.tasks = make(map[float64]Task)
		if err := m.saveTasks(); err != nil {
			return "", fmt.Errorf("failed to save tasks: %w", err)
		}
		return "All tasks cleared.", nil

	default:
		return "", fmt.Errorf("unknown action: %s", action)
	}
}
