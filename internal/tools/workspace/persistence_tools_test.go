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
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
)

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

func (m *mockListStore) Query(ctx context.Context, filter ports.ListFilter, limit, offset int) ([]ports.Task, error) {
	if m.err != nil {
		return nil, m.err
	}
	var result []ports.Task
	for _, t := range m.tasks {
		if filter.Status != "" && t.Status != filter.Status {
			continue
		}
		if filter.NotStatus != "" && t.Status == filter.NotStatus {
			continue
		}
		if !filter.Since.IsZero() && t.CreatedAt.Before(filter.Since) {
			continue
		}
		if !filter.Before.IsZero() && t.CreatedAt.After(filter.Before) {
			continue
		}
		result = append(result, t)
	}
	if offset > 0 {
		if offset >= len(result) {
			return []ports.Task{}, nil
		}
		result = result[offset:]
	}
	if limit > 0 && limit < len(result) {
		result = result[:limit]
	}
	return result, nil
}

func (m *mockListStore) Count(ctx context.Context) (int, error) {
	if m.err != nil {
		return 0, m.err
	}
	return len(m.tasks), nil
}

type mockSessionProvider struct {
	tasks     ports.TaskStore
	info      ports.SessionInfo
	listStore *mockListStore
}

func (m *mockSessionProvider) GetTasks() ports.TaskStore             { return m.tasks }
func (m *mockSessionProvider) GetSettings() ports.KVStore            { return nil }
func (m *mockSessionProvider) GetInfo() ports.SessionInfo            { return m.info }
func (m *mockSessionProvider) SetInfo(info ports.SessionInfo)        { m.info = info }
func (m *mockSessionProvider) Close() error                          { return nil }
func (m *mockSessionProvider) GetHealthChecker() ports.HealthChecker { return nil }

type mockMetadataProvider struct {
	tools.ToolMetadataProvider
	toolkits []string
}

func (m *mockMetadataProvider) ListAvailableToolkits() []string {
	if m.toolkits == nil {
		return []string{"core"}
	}
	return m.toolkits
}

func setupPersistenceTools() (*persistenceTools, *mockSessionProvider) {
	lt := &mockListStore{}
	ts := services.NewTaskService(lt)
	mp := &mockMetadataProvider{}

	provider := &mockSessionProvider{
		tasks:     ts,
		listStore: lt,
		info: ports.SessionInfo{
			Env:   make(map[string]string),
			Paths: make(map[string]string),
		},
	}

	return newpersistenceTools(provider, mp), provider
}

func TestPersistenceTools_GetSessionInfo(t *testing.T) {
	pt, provider := setupPersistenceTools()
	provider.info.Model = "test-model"

	res, err := pt.GetSessionInfo(context.Background(), nil, nil)
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
				if s, ok := ts.(ports.Initializer); ok {
					_ = s.Initialize(context.Background())
				}
			},
			expectedResult: "task 1",
		},
		{
			name: "Successfully update task",
			args: map[string]interface{}{"action": "update", "task_id": 1.0, "status": "completed"},
			setup: func(m *mockListStore, ts ports.TaskStore) {
				m.tasks = []ports.Task{{ID: 1, Content: "task 1", Status: "pending"}}
				if s, ok := ts.(ports.Initializer); ok {
					_ = s.Initialize(context.Background())
				}
			},
			expectedResult: "Task 1 updated",
		},
		{
			name: "Successfully list completed tasks",
			args: map[string]interface{}{"action": "list", "status": "completed"},
			setup: func(m *mockListStore, ts ports.TaskStore) {
				// Use AddTask + UpdateTask to get a completed task into
				// the in-memory map (Initialize no longer loads completed tasks).
				ctx := context.Background()
				t1, err := ts.AddTask(ctx, "task 1")
				if err != nil {
					t.Fatalf("AddTask failed: %v", err)
				}
				if _, err := ts.UpdateTask(ctx, t1.ID, "", "completed"); err != nil {
					t.Fatalf("UpdateTask failed: %v", err)
				}
				if _, err := ts.AddTask(ctx, "task 2"); err != nil {
					t.Fatalf("AddTask failed: %v", err)
				}
			},
			expectedResult: "[x]",
		},
		{
			name: "Successfully delete task",
			args: map[string]interface{}{"action": "delete", "task_id": 1.0},
			setup: func(m *mockListStore, ts ports.TaskStore) {
				m.tasks = []ports.Task{{ID: 1, Content: "task 1", Status: "pending"}}
				if s, ok := ts.(ports.Initializer); ok {
					_ = s.Initialize(context.Background())
				}
			},
			expectedResult: "Task 1 deleted",
		},
		{
			name: "Successfully clear tasks",
			args: map[string]interface{}{"action": "clear"},
			setup: func(m *mockListStore, ts ports.TaskStore) {
				m.tasks = []ports.Task{{ID: 1, Content: "task 1", Status: "pending"}}
				if s, ok := ts.(ports.Initializer); ok {
					_ = s.Initialize(context.Background())
				}
			},
			expectedResult: "All tasks cleared",
		},
		{
			name: "list with default limit",
			args: map[string]interface{}{"action": "list"},
			setup: func(m *mockListStore, ts ports.TaskStore) {
				ctx := context.Background()
				for i := 1; i <= 60; i++ {
					_, _ = ts.AddTask(ctx, fmt.Sprintf("task %d", i))
				}
			},
			expectedResult: "showing 1-50 of 60",
		},
		{
			name: "list with limit and offset",
			args: map[string]interface{}{"action": "list", "offset": 50.0},
			setup: func(m *mockListStore, ts ports.TaskStore) {
				ctx := context.Background()
				for i := 1; i <= 60; i++ {
					_, _ = ts.AddTask(ctx, fmt.Sprintf("task %d", i))
				}
			},
			expectedResult: "showing 51-60 of 60",
		},
		{
			name:           "list empty store",
			args:           map[string]interface{}{"action": "list"},
			expectedResult: "No tasks found.",
		},
		{
			name: "list with next page hint",
			args: map[string]interface{}{"action": "list"},
			setup: func(m *mockListStore, ts ports.TaskStore) {
				ctx := context.Background()
				for i := 1; i <= 60; i++ {
					_, _ = ts.AddTask(ctx, fmt.Sprintf("task %d", i))
				}
			},
			expectedResult: "Use offset=50 for next page.",
		},
		{
			name:           "Error on unknown action",
			args:           map[string]interface{}{"action": "unknown"},
			expectedResult: "Error: unknown action: unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pt, provider := setupPersistenceTools()
			setupManageTasks(t, tt.setup, provider)

			res, err := pt.ManageTasks(context.Background(), tt.args, nil)

			assertManageTasksResult(t, res, err, tt.expectedResult, tt.expectError)
		})
	}
}

func setupManageTasks(t *testing.T, setup func(*mockListStore, ports.TaskStore), provider *mockSessionProvider) {
	t.Helper()
	if setup != nil {
		setup(provider.listStore, provider.tasks)
	}
}

func assertManageTasksResult(t *testing.T, res tools.ToolResult, err error, expectedResult string, expectError bool) {
	t.Helper()

	if expectError {
		if err == nil {
			t.Error("Expected error but got none")
		}
		return
	}

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !strings.Contains(res.Text, expectedResult) {
		t.Errorf("Expected result to contain %q, got %q", expectedResult, res.Text)
	}
}

func TestPersistenceTools_StoreErrors(t *testing.T) {
	pt, provider := setupPersistenceTools()
	ctx := context.Background()

	// Inject error into list store
	provider.listStore.err = fmt.Errorf("list store error")

	_, err := pt.ManageTasks(ctx, map[string]interface{}{"action": "add", "content": "task"}, nil)
	if err == nil {
		t.Error("Expected error from addTask")
	}

	_, err = pt.ManageTasks(ctx, map[string]interface{}{"action": "clear"}, nil)
	if err == nil {
		t.Error("Expected error from clearTasks")
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

	expected := []string{"get_session_info", "manage_tasks"}
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
	pt := newpersistenceTools(nil, nil)
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
	_, err := pt.ManageTasks(ctx, map[string]interface{}{"action": "add", "content": ""}, nil)
	if err == nil {
		t.Error("Expected error for empty content in addTask")
	}

	// updateTask error (not found)
	_, err = pt.ManageTasks(ctx, map[string]interface{}{"action": "update", "task_id": 999.0, "status": "completed"}, nil)
	if err == nil {
		t.Error("Expected error for non-existent task in updateTask")
	}

	// deleteTask error (not found)
	_, err = pt.ManageTasks(ctx, map[string]interface{}{"action": "delete", "task_id": 999.0}, nil)
	if err == nil {
		t.Error("Expected error for non-existent task in deleteTask")
	}
}

func TestNewPersistenceTools_InterfaceNilPointer(t *testing.T) {
	// Create a typed nil pointer stored in an interface variable:
	// the interface itself is non-nil, but the underlying pointer is nil.
	var nilProvider *mockSessionProvider = nil
	var sp ports.SessionProvider = nilProvider // non-nil interface wrapping nil ptr

	pt := newpersistenceTools(sp, nil)
	if pt.state != nil {
		t.Error("Expected nil state when interface wraps nil pointer")
	}

	// Verify no panic and no tools registered
	reg := registry.New()
	if err := pt.Register(reg); err != nil {
		t.Errorf("Register should not fail for interface-nil-pointer: %v", err)
	}
	if len(reg.GetDeclarations()) != 0 {
		t.Error("Expected no tools registered for interface-nil-pointer")
	}
}

// ---------------------------------------------------------------------------
// mockToolRegistrar — minimal ToolRegistrar for testing Register error paths
// ---------------------------------------------------------------------------

type mockToolRegistrar struct {
	failOn  string
	failErr error
	called  []string
}

func (m *mockToolRegistrar) Register(def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	m.called = append(m.called, def.Name)
	if def.Name == m.failOn {
		if m.failErr != nil {
			return m.failErr
		}
		return fmt.Errorf("injected failure for %s", def.Name)
	}
	return nil
}

func (m *mockToolRegistrar) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	m.called = append(m.called, def.Name)
	if def.Name == m.failOn {
		if m.failErr != nil {
			return m.failErr
		}
		return fmt.Errorf("injected failure for %s", def.Name)
	}
	return nil
}

func (m *mockToolRegistrar) RegisterToToolkit(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	return m.Register(def, handler)
}

func (m *mockToolRegistrar) RegisterToToolkitWithOptions(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return m.RegisterWithOptions(def, handler, opts)
}

func TestPersistenceTools_Register_ErrorPaths(t *testing.T) {
	pt, _ := setupPersistenceTools()

	t.Run("get_session_info Register fails", func(t *testing.T) {
		reg := &mockToolRegistrar{failOn: "get_session_info"}
		err := pt.Register(reg)
		if err == nil {
			t.Fatal("expected error from Register")
		}
		if !strings.Contains(err.Error(), "injected failure") {
			t.Errorf("expected 'injected failure', got %q", err.Error())
		}
	})

	t.Run("load_toolkit RegisterWithOptions fails", func(t *testing.T) {
		reg := &mockToolRegistrar{failOn: "load_toolkit"}
		err := pt.Register(reg)
		if err == nil {
			t.Fatal("expected error from Register")
		}
		if !strings.Contains(err.Error(), "injected failure") {
			t.Errorf("expected 'injected failure', got %q", err.Error())
		}
	})

	t.Run("manage_tasks RegisterWithOptions fails", func(t *testing.T) {
		reg := &mockToolRegistrar{failOn: "manage_tasks"}
		err := pt.Register(reg)
		if err == nil {
			t.Fatal("expected error from Register")
		}
		if !strings.Contains(err.Error(), "injected failure") {
			t.Errorf("expected 'injected failure', got %q", err.Error())
		}
	})
}

// TestManageTasks_ListLastPageNoHint verifies that when listing the last
// partial page, no "Use offset=" pagination hint is emitted.
func TestManageTasks_ListLastPageNoHint(t *testing.T) {
	pt, provider := setupPersistenceTools()
	ctx := context.Background()

	// Add 60 tasks
	for i := 1; i <= 60; i++ {
		_, err := provider.tasks.AddTask(ctx, fmt.Sprintf("task %d", i))
		if err != nil {
			t.Fatalf("AddTask failed: %v", err)
		}
	}

	// List with offset=50 (last partial page: tasks 51-60)
	res, err := pt.ManageTasks(ctx, map[string]interface{}{
		"action": "list",
		"offset": 50.0,
	}, nil)
	if err != nil {
		t.Fatalf("ManageTasks failed: %v", err)
	}

	if strings.Contains(res.Text, "Use offset=") {
		t.Errorf("expected no 'Use offset=' hint on last page, got: %s", res.Text)
	}
	if !strings.Contains(res.Text, "showing 51-60 of 60") {
		t.Errorf("expected 'showing 51-60 of 60' header, got: %s", res.Text)
	}
}

// TestManageTasks_UnmarshalError verifies that passing a non-string "action"
// triggers an UnmarshalArgs failure in ManageTasks, exercising the error path
// at line 111-113 in persistence_tools.go.
func TestManageTasks_UnmarshalError(t *testing.T) {
	pt, _ := setupPersistenceTools()
	ctx := context.Background()
	_, err := pt.ManageTasks(ctx, map[string]interface{}{"action": 123}, nil)
	if err == nil {
		t.Fatal("expected error from unmarshal args")
	}
}
