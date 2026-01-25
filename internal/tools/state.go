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

	"google.golang.org/genai"
)

// Task represents a single item in the task manager, matching the Bash version's schema.
type Task struct {
	ID      int    `json:"id"`
	Content string `json:"content"`
	Status  string `json:"status"`
}

// RegisterStateTools adds scratchpad and task management tools.
func RegisterStateTools(r *Registry, homeDir string, sessionName string) {
	// We pass homeDir and sessionName to closures so the handlers know where to look.
	
	r.Register(&genai.FunctionDeclaration{
		Name:        "manage_scratchpad",
		Description: "Read, write, or update the persistent session scratchpad. Use this to keep track of plans, completed tasks, or architectural notes.",
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
				"scope": {
					Type:        genai.TypeString,
					Description: "The scope of the scratchpad: 'session' (default) or 'global'.",
					Enum:        []string{"session", "global"},
				},
			},
			Required: []string{"action"},
		},
	}, func(args map[string]interface{}) (string, error) {
		return manageScratchpad(args, homeDir, sessionName)
	})

	r.Register(&genai.FunctionDeclaration{
		Name:        "manage_tasks",
		Description: "Manages a todo list of tasks. Supports adding, updating, listing, and deleting tasks.",
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
				"scope": {
					Type:        genai.TypeString,
					Description: "The scope of the task list: 'session' (default) or 'global'.",
					Enum:        []string{"session", "global"},
				},
			},
			Required: []string{"action"},
		},
	}, func(args map[string]interface{}) (string, error) {
		return manageTasks(args, homeDir, sessionName)
	})
}

func getScratchpadPath(homeDir, sessionName, scope string) string {
	if scope == "global" {
		return filepath.Join(homeDir, "output", "global-scratchpad.md")
	}
	return filepath.Join(homeDir, "output", sessionName+".scratchpad.md")
}

func getTasksPath(homeDir, sessionName, scope string) string {
	if scope == "global" {
		return filepath.Join(homeDir, "output", "global-tasks.json")
	}
	return filepath.Join(homeDir, "output", sessionName+".tasks.json")
}

func manageScratchpad(args map[string]interface{}, homeDir, sessionName string) (string, error) {
	action, _ := args["action"].(string)
	content, _ := args["content"].(string)
	scope, _ := args["scope"].(string)
	if scope == "" {
		scope = "session"
	}

	path := getScratchpadPath(homeDir, sessionName, scope)
	
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
		err := os.WriteFile(path, []byte(content), 0644)
		if err != nil {
			return "", fmt.Errorf("failed to write scratchpad: %w", err)
		}
		return "Scratchpad overwritten.", nil

	case "append":
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return "", fmt.Errorf("failed to open scratchpad for append: %w", err)
		}
		defer f.Close()
		
		stat, _ := f.Stat()
		if stat.Size() > 0 {
			_, _ = f.WriteString("\n")
		}
		_, err = f.WriteString(content)
		if err != nil {
			return "", fmt.Errorf("failed to append to scratchpad: %w", err)
		}
		return "Content appended to scratchpad.", nil

	case "clear":
		_ = os.WriteFile(path, []byte(""), 0644)
		return "Scratchpad cleared.", nil
	}

	return "Invalid action", nil
}

func manageTasks(args map[string]interface{}, homeDir, sessionName string) (string, error) {
	action, _ := args["action"].(string)
	scope, _ := args["scope"].(string)
	if scope == "" {
		scope = "session"
	}

	path := getTasksPath(homeDir, sessionName, scope)
	_ = os.MkdirAll(filepath.Dir(path), 0755)

	// Load existing tasks
	var tasks []Task
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &tasks)
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
		saveTasks(path, tasks)
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
		saveTasks(path, tasks)
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
		saveTasks(path, newTasks)
		return fmt.Sprintf("Task %d deleted.", id), nil

	case "clear":
		saveTasks(path, []Task{})
		return "All tasks cleared.", nil
	}

	return "Invalid action", nil
}

func saveTasks(path string, tasks []Task) {
	data, _ := json.MarshalIndent(tasks, "", "  ")
	_ = os.WriteFile(path, data, 0644)
}

