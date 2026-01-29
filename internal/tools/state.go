// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/history"
	"google.golang.org/genai"
)

var (
	taskMu       sync.Mutex
	scratchpadMu sync.Mutex
	configMu     sync.Mutex
)

// Task represents a single item in the task manager, matching the Bash version's schema.
type Task struct {
	ID      int    `json:"id"`
	Content string `json:"content"`
	Status  string `json:"status"`
}

// RegisterStateTools adds scratchpad, task management, and session info tools.
func RegisterStateTools(r *Registry, homeDir string, hManager *history.Manager, mode string) {
	// We pass homeDir to closures so the tool functions know where to look.

	r.Register(&genai.FunctionDeclaration{
		Name:        "get_session_info",
		Description: "Returns the active configuration, environment variables, and session file paths to help the agent understand its identity and constraints.",
	}, func(args map[string]interface{}) (string, error) {
		info := map[string]interface{}{
			"home_dir":           homeDir,
			"safe_paths":         GetSafePaths(),
			"bypass_active":      IsBypassActive(),
			"history_file":       hManager.GetPath(),
			"active_config_path": "", // Will be filled if found in safe paths
		}

		// Try to identify the config path from safe paths (usually the 2nd one registered in main.go)
		paths := GetSafePaths()
		for _, p := range paths {
			if strings.HasSuffix(p, ".yaml") || strings.HasSuffix(p, ".yml") {
				info["active_config_path"] = p
				break
			}
		}

		b, err := json.MarshalIndent(info, "", "  ")
		if err != nil {
			return "", err
		}
		return string(b), nil
	})

	r.RegisterWithOptions(&genai.FunctionDeclaration{
		Name:        "manage_scratchpad",
		Description: "Read, write, or update the persistent scratchpad (scoped to current mode). Use this to keep track of plans, completed tasks, or architectural notes across sessions.",
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
	}, func(args map[string]interface{}) (string, error) {
		return manageScratchpad(args, homeDir, mode)
	}, ToolOptions{Serial: true})

	r.RegisterWithOptions(&genai.FunctionDeclaration{
		Name:        "manage_config",
		Description: "Manages persistent key-value configuration/settings scoped by mode. Useful for storing URLs, IDs, or preferences across sessions.",
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
	}, func(args map[string]interface{}) (string, error) {
		return manageConfig(args, homeDir, mode)
	}, ToolOptions{Serial: true})

	r.RegisterWithOptions(&genai.FunctionDeclaration{
		Name: "configure_ux_preferences",
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
	}, func(args map[string]interface{}) (string, error) {
		feature, _ := args["feature"].(string)
		status, _ := args["status"].(string)

		configArgs := map[string]interface{}{
			"action": "set",
			"key":    feature,
			"value":  status,
		}
		return manageConfig(configArgs, homeDir, mode)
	}, ToolOptions{Serial: true})

	r.RegisterWithOptions(&genai.FunctionDeclaration{
		Name:        "manage_tasks",
		Description: "Manages a to-do list of tasks (scoped to current mode). Supports adding, updating, listing, and deleting tasks.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"action": {
					Type:        genai.TypeString,
					Description: "The action to perform: 'add', 'update', 'list', 'delete', 'clear'.",
					Enum:        []string{"add", "update", "list", "delete", "clear"},
				},
				"content": {
					Type:        genai.TypeString,
					Description: "The task description (required for 'add').",
				},
				"task_id": {
					Type:        genai.TypeNumber,
					Description: "The ID of the task to update or delete.",
				},
				"status": {
					Type:        genai.TypeString,
					Description: "The new status (e.g., 'completed', 'pending') for 'update' or filter for 'list'.",
				},
			},
			Required: []string{"action"},
		},
	}, func(args map[string]interface{}) (string, error) {
		return manageTasks(args, homeDir, mode)
	}, ToolOptions{Serial: true})

	r.RegisterWithOptions(&genai.FunctionDeclaration{
		Name:        "rollback_last_turn",
		Description: "Reverts the conversation history to the state before the current turn. This effectively undoes the last interaction.",
	}, func(args map[string]interface{}) (string, error) {
		if hManager != nil {
			hManager.Rollback()
			return "Rollback successful. History restored to previous snapshot.", nil
		}
		return "Error: History manager not available for rollback.", nil
	}, ToolOptions{Serial: true})
}

func getScratchpadPath(homeDir, mode string) string {
	return filepath.Join(homeDir, "output", fmt.Sprintf("%s_scratchpad.md", mode))
}

func getTasksPath(homeDir, mode string) string {
	return filepath.Join(homeDir, "output", fmt.Sprintf("%s_tasks.json", mode))
}

func getConfigPath(homeDir, mode string) string {
	return filepath.Join(homeDir, "output", fmt.Sprintf("%s_config.json", mode))
}

func manageConfig(args map[string]interface{}, homeDir, mode string) (string, error) {
	configMu.Lock()
	defer configMu.Unlock()
	action, _ := args["action"].(string)
	key, _ := args["key"].(string)
	value, _ := args["value"].(string)

	path := getConfigPath(homeDir, mode)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}

	config := make(map[string]string)
	data, err := os.ReadFile(path)
	if err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &config); err != nil {
			TerminalMutex.Lock()
			fmt.Fprintf(os.Stderr, "Warning: Config file %s is corrupted. Renaming to .bak and resetting.\n", path)
			TerminalMutex.Unlock()
			_ = os.Rename(path, path+".bak")
			config = make(map[string]string)
		}
	}

	switch action {
	case "set":
		if key == "" {
			return "Error: 'key' is required for 'set'", nil
		}
		config[key] = value
		newData, _ := json.MarshalIndent(config, "", "  ")
		if err := AtomicWrite(path, newData, 0644); err != nil {
			return "", err
		}
		return fmt.Sprintf("Configuration '%s' set successfully.", key), nil

	case "get":
		if key == "" {
			return "Error: 'key' is required for 'get'", nil
		}
		val, ok := config[key]
		if !ok {
			return fmt.Sprintf("Configuration key '%s' not found.", key), nil
		}
		return val, nil

	case "list":
		if len(config) == 0 {
			return "No configuration found for this mode.", nil
		}
		var lines []string
		for k, v := range config {
			lines = append(lines, fmt.Sprintf("- %s: %s", k, v))
		}
		sort.Strings(lines)
		return "Persistent Configuration:\n" + strings.Join(lines, "\n"), nil

	case "delete":
		if key == "" {
			return "Error: 'key' is required for 'delete'", nil
		}
		if _, ok := config[key]; !ok {
			return fmt.Sprintf("Key '%s' not found.", key), nil
		}
		delete(config, key)
		newData, _ := json.MarshalIndent(config, "", "  ")
		if err := AtomicWrite(path, newData, 0644); err != nil {
			return "", err
		}
		return fmt.Sprintf("Configuration key '%s' deleted.", key), nil
	}

	return "Invalid action", nil
}

func manageScratchpad(args map[string]interface{}, homeDir, mode string) (string, error) {
	scratchpadMu.Lock()
	defer scratchpadMu.Unlock()
	action, _ := args["action"].(string)
	content, _ := args["content"].(string)

	path := getScratchpadPath(homeDir, mode)

	// Ensure directory exists
	_ = os.MkdirAll(filepath.Dir(path), 0755)

	switch action {
	case "read":
		data, err := os.ReadFile(path)
		if err != nil {
			return "[Scratchpad does not exist yet]", nil
		}
		if len(data) == 0 {
			return "[Scratchpad is empty]", nil
		}
		return string(data), nil

	case "write":
		err := AtomicWrite(path, []byte(content), 0644)
		if err != nil {
			return "", fmt.Errorf("failed to write scratchpad: %w", err)
		}
		return "Scratchpad overwritten.", nil

	case "append":
		existing, _ := os.ReadFile(path)
		var newContent []byte
		if len(existing) > 0 {
			newContent = append(existing, '\n')
			newContent = append(newContent, []byte(content)...)
		} else {
			newContent = []byte(content)
		}
		err := AtomicWrite(path, newContent, 0644)
		if err != nil {
			return "", fmt.Errorf("failed to append to scratchpad: %w", err)
		}
		return "Content appended to scratchpad.", nil

	case "clear":
		_ = AtomicWrite(path, []byte(""), 0644)
		return "Scratchpad cleared.", nil
	}

	return "Invalid action", nil
}

func manageTasks(args map[string]interface{}, homeDir, mode string) (string, error) {
	taskMu.Lock()
	defer taskMu.Unlock()
	action, _ := args["action"].(string)

	path := getTasksPath(homeDir, mode)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", fmt.Errorf("failed to create tasks directory: %w", err)
	}

	// Load existing tasks
	var tasks []Task
	data, err := os.ReadFile(path)
	if err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &tasks); err != nil {
			TerminalMutex.Lock()
			fmt.Fprintf(os.Stderr, "Warning: Tasks file %s is corrupted. Renaming to .bak and resetting.\n", path)
			TerminalMutex.Unlock()
			_ = os.Rename(path, path+".bak")
			tasks = []Task{}
		}
	}

	switch action {
	case "add":
		content, _ := args["content"].(string)
		if content == "" {
			return "Error: 'content' is required for 'add'", nil
		}
		nextID := 1
		for _, t := range tasks {
			if t.ID >= nextID {
				nextID = t.ID + 1
			}
		}
		tasks = append(tasks, Task{ID: nextID, Content: content, Status: "pending"})
		if err := saveTasks(path, tasks); err != nil {
			return "", err
		}
		return fmt.Sprintf("Task added with ID: %d", nextID), nil

	case "list":
		statusFilter, _ := args["status"].(string)
		var lines []string
		for _, t := range tasks {
			if statusFilter == "" || t.Status == statusFilter {
				lines = append(lines, fmt.Sprintf("[%d] [%s] %s", t.ID, t.Status, t.Content))
			}
		}
		if len(lines) == 0 {
			return "No tasks found.", nil
		}
		sort.Strings(lines)
		return "Current Tasks:\n" + strings.Join(lines, "\n"), nil

	case "update":
		idFloat, _ := args["task_id"].(float64)
		id := int(idFloat)
		content, _ := args["content"].(string)
		status, _ := args["status"].(string)
		found := false
		for i, t := range tasks {
			if t.ID == id {
				if content != "" {
					tasks[i].Content = content
				}
				if status != "" {
					tasks[i].Status = status
				}
				found = true
				break
			}
		}
		if !found {
			return fmt.Sprintf("Error: Task ID %d not found.", id), nil
		}
		if err := saveTasks(path, tasks); err != nil {
			return "", err
		}
		return fmt.Sprintf("Task %d updated.", id), nil

	case "delete":
		idFloat, _ := args["task_id"].(float64)
		id := int(idFloat)
		newTasks := []Task{}
		found := false
		for _, t := range tasks {
			if t.ID == id {
				found = true
				continue
			}
			newTasks = append(newTasks, t)
		}
		if !found {
			return fmt.Sprintf("Error: Task ID %d not found.", id), nil
		}
		if err := saveTasks(path, newTasks); err != nil {
			return "", err
		}
		return fmt.Sprintf("Task %d deleted.", id), nil

	case "clear":
		if err := saveTasks(path, []Task{}); err != nil {
			return "", err
		}
		return "All tasks cleared.", nil
	}

	return "Invalid action", nil
}

func saveTasks(path string, tasks []Task) error {
	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal tasks: %w", err)
	}
	return AtomicWrite(path, data, 0644)
}
