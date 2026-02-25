// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
)

// mockKVStore implements services.KVStore
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

// mockListStore implements services.ListStore[services.Task]
type mockListStore struct {
	tasks []services.Task
	err   error
}

func (m *mockListStore) ReadAll(ctx context.Context) ([]services.Task, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.tasks, nil
}

func (m *mockListStore) ReadPage(ctx context.Context, limit, offset int) ([]services.Task, error) {
	return nil, nil
}

func (m *mockListStore) Append(ctx context.Context, item services.Task) error {
	if m.err != nil {
		return m.err
	}
	m.tasks = append(m.tasks, item)
	return nil
}

func (m *mockListStore) Update(ctx context.Context, id float64, item services.Task) error {
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
	tasks      *services.TaskService
	config     *services.ConfigService
	scratchpad *services.ScratchpadService
	info       services.SessionInfo
	kvStore    *mockKVStore
	listStore  *mockListStore
}

func (m *mockSessionProvider) GetTasks() *services.TaskService      { return m.tasks }
func (m *mockSessionProvider) GetConfig() *services.ConfigService    { return m.config }
func (m *mockSessionProvider) GetScratchpad() *services.ScratchpadService { return m.scratchpad }
func (m *mockSessionProvider) GetInfo() services.SessionInfo        { return m.info }
func (m *mockSessionProvider) SetInfo(info services.SessionInfo)    { m.info = info }
func (m *mockSessionProvider) Close() error                         { return nil }

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
		info: services.SessionInfo{
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
	
	var info services.SessionInfo
	if err := json.Unmarshal([]byte(res.Text), &info); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}
	
	if info.Model != "test-model" {
		t.Errorf("Expected model test-model, got %s", info.Model)
	}
}

func TestPersistenceTools_ManageTasks(t *testing.T) {
	pt, _ := setupPersistenceTools()
	ctx := context.Background()

	// Add
	res, err := pt.ManageTasks(ctx, map[string]interface{}{"action": "add", "content": "task 1"})
	if err != nil {
		t.Fatalf("Add task failed: %v", err)
	}
	if !strings.Contains(res.Text, "Task added") {
		t.Errorf("Expected success message, got %s", res.Text)
	}

	// List
	res, err = pt.ManageTasks(ctx, map[string]interface{}{"action": "list"})
	if err != nil {
		t.Fatalf("List tasks failed: %v", err)
	}
	if !strings.Contains(res.Text, "task 1") {
		t.Errorf("Expected task 1 in list, got %s", res.Text)
	}

	// Update
	res, err = pt.ManageTasks(ctx, map[string]interface{}{"action": "update", "task_id": 1.0, "status": "completed"})
	if err != nil {
		t.Fatalf("Update task failed: %v", err)
	}

	// List again to verify update
	res, err = pt.ManageTasks(ctx, map[string]interface{}{"action": "list", "status": "completed"})
	if err != nil {
		t.Fatalf("List tasks failed: %v", err)
	}
	if !strings.Contains(res.Text, "[x]") {
		t.Errorf("Expected completed task icon, got %s", res.Text)
	}

	// Delete
	res, err = pt.ManageTasks(ctx, map[string]interface{}{"action": "delete", "task_id": 1.0})
	if err != nil {
		t.Fatalf("Delete task failed: %v", err)
	}

	// Clear
	res, err = pt.ManageTasks(ctx, map[string]interface{}{"action": "clear"})
	if err != nil {
		t.Fatalf("Clear tasks failed: %v", err)
	}
	
	// Unknown action
	_, err = pt.ManageTasks(ctx, map[string]interface{}{"action": "unknown"})
	if err == nil {
		t.Error("Expected error for unknown action")
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
	pt.Register(reg)

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
	RegisterPersistence(reg, provider)

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
	pt.Register(reg) // Should not panic
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
	pt.ManageConfig(ctx, map[string]interface{}{"action": "set", "key": "k", "value": "v"})
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
