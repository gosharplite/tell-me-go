// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
)

// mockKVStore implements ports.KVStore
type mockKVStore struct {
	kv  map[string]string
	err error
}

func (m *mockKVStore) Get(ctx context.Context, key string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	if v, ok := m.kv[key]; ok {
		return v, nil
	}
	return "", nil
}

func (m *mockKVStore) Set(ctx context.Context, key string, val string) error {
	if m.err != nil {
		return m.err
	}
	m.kv[key] = val
	return nil
}

func (m *mockKVStore) Delete(ctx context.Context, key string) error {
	if m.err != nil {
		return m.err
	}
	delete(m.kv, key)
	return nil
}

func (m *mockKVStore) GetAll(ctx context.Context) (map[string]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	res := make(map[string]string)
	for k, v := range m.kv {
		res[k] = v
	}
	return res, nil
}

// mockListStore implements ports.ListStore[ports.Task]
type mockListStore struct {
	tasks []ports.Task
	err   error
}

func (m *mockListStore) ReadAll(ctx context.Context) ([]ports.Task, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.tasks, nil
}

func (m *mockListStore) Append(ctx context.Context, item ports.Task) error {
	if m.err != nil {
		return m.err
	}
	m.tasks = append(m.tasks, item)
	return nil
}

func (m *mockListStore) Update(ctx context.Context, id float64, item ports.Task) error {
	if m.err != nil {
		return m.err
	}
	for i, t := range m.tasks {
		if t.ID == id {
			m.tasks[i] = item
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (m *mockListStore) Delete(ctx context.Context, id float64) error {
	if m.err != nil {
		return m.err
	}
	for i, t := range m.tasks {
		if t.ID == id {
			m.tasks = append(m.tasks[:i], m.tasks[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (m *mockListStore) DeleteAll(ctx context.Context) error {
	if m.err != nil {
		return m.err
	}
	m.tasks = nil
	return nil
}

type mockSessionProvider struct {
	tasks      ports.TaskStore
	config     ports.ConfigService
	scratchpad ports.ScratchpadService
	info       ports.SessionInfo
	kvStore    *mockKVStore
	listStore  *mockListStore
}

func (m *mockSessionProvider) GetTasks() ports.TaskStore              { return m.tasks }
func (m *mockSessionProvider) GetConfig() ports.ConfigService         { return m.config }
func (m *mockSessionProvider) GetScratchpad() ports.ScratchpadService { return m.scratchpad }
func (m *mockSessionProvider) GetInfo() ports.SessionInfo             { return m.info }
func (m *mockSessionProvider) SetInfo(info ports.SessionInfo)         { m.info = info }
func (m *mockSessionProvider) Close() error                           { return nil }

func setupPersistenceTools() (*persistenceTools, *mockSessionProvider) {
	kv := &mockKVStore{kv: make(map[string]string)}
	lt := &mockListStore{}
	ts := services.NewTaskService(lt)
	cs := services.NewConfigService(kv)
	ss := services.NewScratchpadService(kv)

	provider := &mockSessionProvider{
		tasks:      ts,
		config:     cs,
		scratchpad: ss,
		kvStore:    kv,
		listStore:  lt,
		info: ports.SessionInfo{
			Config: make(map[string]string),
			Env:    make(map[string]string),
			Paths:  make(map[string]string),
		},
	}

	return newpersistenceTools(provider), provider
}

func TestPersistenceTools_GetSessionInfo(t *testing.T) {
	pt, provider := setupPersistenceTools()
	provider.info.Model = "test-model"

	res, err := pt.GetSessionInfo(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetSessionInfo failed: %v", err)
	}

	var info ports.SessionInfo
	if err := json.Unmarshal([]byte(res.Text), &info); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if info.Model != "test-model" {
		t.Errorf("Expected model test-model, got %s", info.Model)
	}
}

func TestPersistenceTools_ManageTasks(t *testing.T) {
	tests := []struct {
		name           string
		args           map[string]interface{}
		setup          func(*mockListStore, ports.TaskStore)
		expectedResult string
		expectError    bool
	}{
		{
			name:           "Successfully add task",
			args:           map[string]interface{}{"action": "add", "content": "task 1"},
			expectedResult: "Task added with ID 1",
		},
		{
			name: "Successfully list tasks",
			args: map[string]interface{}{"action": "list"},
			setup: func(m *mockListStore, ts ports.TaskStore) {
				m.tasks = []ports.Task{{ID: 1, Content: "task 1", Status: "pending"}}
				_ = ts.Initialize(context.Background())
			},
			expectedResult: "task 1",
		},
		{
			name: "Successfully update task",
			args: map[string]interface{}{"action": "update", "task_id": 1.0, "status": "completed"},
			setup: func(m *mockListStore, ts ports.TaskStore) {
				m.tasks = []ports.Task{{ID: 1, Content: "task 1", Status: "pending"}}
				_ = ts.Initialize(context.Background())
			},
			expectedResult: "Task 1 updated",
		},
		{
			name: "Successfully list completed tasks",
			args: map[string]interface{}{"action": "list", "status": "completed"},
			setup: func(m *mockListStore, ts ports.TaskStore) {
				m.tasks = []ports.Task{
					{ID: 1, Content: "task 1", Status: "completed"},
					{ID: 2, Content: "task 2", Status: "pending"},
				}
				_ = ts.Initialize(context.Background())
			},
			expectedResult: "[x]",
		},
		{
			name: "Successfully delete task",
			args: map[string]interface{}{"action": "delete", "task_id": 1.0},
			setup: func(m *mockListStore, ts ports.TaskStore) {
				m.tasks = []ports.Task{{ID: 1, Content: "task 1", Status: "pending"}}
				_ = ts.Initialize(context.Background())
			},
			expectedResult: "Task 1 deleted",
		},
		{
			name: "Successfully clear tasks",
			args: map[string]interface{}{"action": "clear"},
			setup: func(m *mockListStore, ts ports.TaskStore) {
				m.tasks = []ports.Task{{ID: 1, Content: "task 1", Status: "pending"}}
				_ = ts.Initialize(context.Background())
			},
			expectedResult: "All tasks cleared",
		},
		{
			name:        "Error on unknown action",
			args:        map[string]interface{}{"action": "unknown"},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pt, provider := setupPersistenceTools()
			if tt.setup != nil {
				tt.setup(provider.listStore, provider.tasks)
			}
			ctx := context.Background()
			res, err := pt.ManageTasks(ctx, tt.args)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if !strings.Contains(res.Text, tt.expectedResult) {
				t.Errorf("Expected result to contain %q, got %q", tt.expectedResult, res.Text)
			}
		})
	}
}

func TestPersistenceTools_ManageScratchpad(t *testing.T) {
	pt, _ := setupPersistenceTools()
	ctx := context.Background()

	// Write
	_, err := pt.ManageScratchpad(ctx, map[string]interface{}{"action": "write", "content": "hello"})
	if err != nil {
		t.Fatalf("Write scratchpad failed: %v", err)
	}

	// Read
	res, err := pt.ManageScratchpad(ctx, map[string]interface{}{"action": "read"})
	if err != nil {
		t.Fatalf("Read scratchpad failed: %v", err)
	}
	if res.Text != "hello" {
		t.Errorf("Expected hello, got %s", res.Text)
	}

	// Append
	_, err = pt.ManageScratchpad(ctx, map[string]interface{}{"action": "append", "content": "world"})
	if err != nil {
		t.Fatalf("Append scratchpad failed: %v", err)
	}

	// Read again
	res, err = pt.ManageScratchpad(ctx, map[string]interface{}{"action": "read"})
	if err != nil {
		t.Fatalf("Read scratchpad failed: %v", err)
	}
	if res.Text != "hello\nworld" {
		t.Errorf("Expected hello\nworld, got %s", res.Text)
	}

	// Clear
	_, err = pt.ManageScratchpad(ctx, map[string]interface{}{"action": "clear"})
	if err != nil {
		t.Fatalf("Clear scratchpad failed: %v", err)
	}

	// Read empty
	res, err = pt.ManageScratchpad(ctx, map[string]interface{}{"action": "read"})
	if err != nil {
		t.Fatalf("Read empty scratchpad failed: %v", err)
	}
	if !strings.Contains(res.Text, "empty") {
		t.Errorf("Expected empty message, got %s", res.Text)
	}
}

func TestPersistenceTools_ManageConfig(t *testing.T) {
	pt, _ := setupPersistenceTools()
	ctx := context.Background()

	// Set
	_, err := pt.ManageConfig(ctx, map[string]interface{}{"action": "set", "key": "foo", "value": "bar"})
	if err != nil {
		t.Fatalf("Set config failed: %v", err)
	}

	// Get
	res, err := pt.ManageConfig(ctx, map[string]interface{}{"action": "get", "key": "foo"})
	if err != nil {
		t.Fatalf("Get config failed: %v", err)
	}
	if res.Text != "bar" {
		t.Errorf("Expected bar, got %s", res.Text)
	}

	// List
	res, err = pt.ManageConfig(ctx, map[string]interface{}{"action": "list"})
	if err != nil {
		t.Fatalf("List config failed: %v", err)
	}
	if !strings.Contains(res.Text, "foo = bar") {
		t.Errorf("Expected foo = bar in list, got %s", res.Text)
	}

	// Delete
	_, err = pt.ManageConfig(ctx, map[string]interface{}{"action": "delete", "key": "foo"})
	if err != nil {
		t.Fatalf("Delete config failed: %v", err)
	}

	// List empty
	res, err = pt.ManageConfig(ctx, map[string]interface{}{"action": "list"})
	if err != nil {
		t.Fatalf("List empty config failed: %v", err)
	}
	if !strings.Contains(res.Text, "empty") {
		t.Errorf("Expected empty message, got %s", res.Text)
	}
}

func TestPersistenceTools_Register(t *testing.T) {
	pt, _ := setupPersistenceTools()
	reg := registry.New()
	if err := pt.Register(reg); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	decls := reg.GetDeclarations()
	found := make(map[string]bool)
	for _, d := range decls {
		found[d.Name] = true
	}

	expected := []string{"get_session_info", "manage_scratchpad", "manage_config", "manage_tasks"}
	for _, name := range expected {
		if !found[name] {
			t.Errorf("Tool %s not registered", name)
		}
	}
}

func TestRegisterPersistence(t *testing.T) {
	reg := registry.New()
	_, provider := setupPersistenceTools()
	if err := RegisterPersistence(reg, provider); err != nil {
		t.Fatalf("RegisterPersistence failed: %v", err)
	}

	decls := reg.GetDeclarations()
	if len(decls) == 0 {
		t.Error("No tools registered")
	}
}

func TestNewPersistenceTools_Nil(t *testing.T) {
	pt := newpersistenceTools(nil)
	if pt.state != nil {
		t.Error("Expected nil state")
	}

	reg := registry.New()
	if err := pt.Register(reg); err != nil {
		t.Errorf("Register should not fail for nil state: %v", err)
	}
	if len(reg.GetDeclarations()) != 0 {
		t.Error("Expected no tools registered for nil state")
	}
}

func TestPersistenceTools_Errors(t *testing.T) {
	pt, _ := setupPersistenceTools()
	ctx := context.Background()

	// addTask error (empty content)
	_, err := pt.ManageTasks(ctx, map[string]interface{}{"action": "add", "content": ""})
	if err == nil {
		t.Error("Expected error for empty content in addTask")
	}

	// updateTask error (not found)
	_, err = pt.ManageTasks(ctx, map[string]interface{}{"action": "update", "task_id": 999.0, "status": "completed"})
	if err == nil {
		t.Error("Expected error for non-existent task in updateTask")
	}

	// deleteTask error (not found)
	_, err = pt.ManageTasks(ctx, map[string]interface{}{"action": "delete", "task_id": 999.0})
	if err == nil {
		t.Error("Expected error for non-existent task in deleteTask")
	}

	// getConfig error (not found)
	_, err = pt.ManageConfig(ctx, map[string]interface{}{"action": "get", "key": "nonexistent"})
	if err == nil {
		t.Error("Expected error for non-existent config key")
	}
}

func TestPersistenceTools_StoreErrors(t *testing.T) {
	pt, provider := setupPersistenceTools()
	ctx := context.Background()

	// Inject error into list store
	provider.listStore.err = fmt.Errorf("list store error")

	_, err := pt.ManageTasks(ctx, map[string]interface{}{"action": "add", "content": "task"})
	if err == nil {
		t.Error("Expected error from addTask")
	}

	_, err = pt.ManageTasks(ctx, map[string]interface{}{"action": "clear"})
	if err == nil {
		t.Error("Expected error from clearTasks")
	}

	// Inject error into KV store
	provider.kvStore.err = nil
	_, _ = pt.ManageConfig(ctx, map[string]interface{}{"action": "set", "key": "k", "value": "v"})
	provider.kvStore.err = fmt.Errorf("kv store error")

	_, err = pt.ManageScratchpad(ctx, map[string]interface{}{"action": "write", "content": "hello"})
	if err == nil {
		t.Error("Expected error from writeScratchpad")
	}

	_, err = pt.ManageScratchpad(ctx, map[string]interface{}{"action": "append", "content": "world"})
	if err == nil {
		t.Error("Expected error from appendScratchpad")
	}

	_, err = pt.ManageScratchpad(ctx, map[string]interface{}{"action": "clear"})
	if err == nil {
		t.Error("Expected error from clearScratchpad")
	}

	_, err = pt.ManageConfig(ctx, map[string]interface{}{"action": "set", "key": "k2", "value": "v"})
	if err == nil {
		t.Error("Expected error from setConfig")
	}

	_, err = pt.ManageConfig(ctx, map[string]interface{}{"action": "delete", "key": "k"})
	if err == nil {
		t.Error("Expected error from deleteConfig")
	}
}
