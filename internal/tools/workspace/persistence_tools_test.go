// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
)

// taskMatchesFilter returns true if task matches the given filter.
func taskMatchesFilter(task ports.Task, filter ports.ListFilter) bool {
	if filter.NotStatus != "" && task.Status == filter.NotStatus {
		return false
	}
	if filter.Status != "" && task.Status != filter.Status {
		return false
	}
	if !filter.Since.IsZero() && task.CreatedAt.Before(filter.Since) {
		return false
	}
	if !filter.Before.IsZero() && task.CreatedAt.After(filter.Before) {
		return false
	}
	return true
}

// applyTaskOffsetLimit applies offset and limit to a slice of tasks.
func applyTaskOffsetLimit(tasks []ports.Task, limit, offset int) []ports.Task {
	if offset >= len(tasks) {
		return []ports.Task{}
	}
	if limit > 0 {
		return tasks[offset:min(offset+limit, len(tasks))]
	}
	return tasks[offset:]
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

func (m *mockListStore) Update(ctx context.Context, id int64, item ports.Task) error {
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

func (m *mockListStore) Delete(ctx context.Context, id int64) error {
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
		if taskMatchesFilter(t, filter) {
			result = append(result, t)
		}
	}
	return applyTaskOffsetLimit(result, limit, offset), nil
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
func (m *mockSessionProvider) SetInfo(_ context.Context, info ports.SessionInfo) error {
	m.info = info
	return nil
}
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

// busyFunc returns a function that returns a SQLITE_BUSY error for the
// first n calls, then returns either success (nil) or the given finalErr.
func busyFunc(busyCount int, finalErr error) func() error {
	calls := 0
	return func() error {
		calls++
		if calls <= busyCount {
			return fmt.Errorf("database is locked (SQLITE_BUSY)")
		}
		return finalErr
	}
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
	tests := []struct {
		name        string
		setup       func(*persistenceTools, *mockSessionProvider)
		wantModel   string
		wantErr     bool
		wantErrText string
	}{
		{
			name: "success",
			setup: func(pt *persistenceTools, p *mockSessionProvider) {
				p.info.Model = "test-model"
			},
			wantModel: "test-model",
		},
		{
			name: "marshal error",
			setup: func(pt *persistenceTools, p *mockSessionProvider) {
				pt.marshalIndent = func(v any, prefix, indent string) ([]byte, error) {
					return nil, errors.New("marshal exploded")
				}
			},
			wantErr:     true,
			wantErrText: "marshal exploded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pt, provider := setupPersistenceTools()
			tt.setup(pt, provider)

			res, err := pt.GetSessionInfo(context.Background(), nil, nil)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErrText) {
					t.Errorf("error = %q; want containing %q", err.Error(), tt.wantErrText)
				}
				// Verify ToolResult is zero-valued on error
				if res.Text != "" {
					t.Errorf("expected empty result on error, got Text=%q", res.Text)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var info ports.SessionInfo
			if err := json.Unmarshal([]byte(res.Text), &info); err != nil {
				t.Fatalf("failed to unmarshal result: %v", err)
			}
			if info.Model != tt.wantModel {
				t.Errorf("Model = %q; want %q", info.Model, tt.wantModel)
			}
		})
	}
}

func TestPersistenceTools_ManageTasks(t *testing.T) {
	t.Run("add", testManageTasksAdd)
	t.Run("list", testManageTasksList)
	t.Run("update", testManageTasksUpdate)
	t.Run("delete_and_clear", testManageTasksDeleteAndClear)
	t.Run("completed_filter", testManageTasksCompletedFilter)
	t.Run("error", testManageTasksError)
}

func testManageTasksAdd(t *testing.T) {
	tests := []struct {
		name           string
		args           map[string]interface{}
		expectedResult string
	}{
		{
			name:           "Successfully add task",
			args:           map[string]interface{}{"action": "add", "content": "task 1"},
			expectedResult: "Task added with ID 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pt, provider := setupPersistenceTools()
			setupManageTasks(t, nil, provider)

			res, err := pt.ManageTasks(context.Background(), tt.args, nil)

			assertManageTasksResult(t, res, err, tt.expectedResult, false)
		})
	}
}

func testManageTasksList(t *testing.T) {
	tests := []struct {
		name           string
		args           map[string]interface{}
		setup          func(*mockListStore, ports.TaskStore)
		expectedResult string
	}{
		{
			name: "list basic",
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
			args: map[string]interface{}{"action": "list", "offset": 50},
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pt, provider := setupPersistenceTools()
			setupManageTasks(t, tt.setup, provider)

			res, err := pt.ManageTasks(context.Background(), tt.args, nil)

			assertManageTasksResult(t, res, err, tt.expectedResult, false)
		})
	}
}

func testManageTasksUpdate(t *testing.T) {
	tests := []struct {
		name           string
		args           map[string]interface{}
		setup          func(*mockListStore, ports.TaskStore)
		expectedResult string
	}{
		{
			name: "Successfully update task",
			args: map[string]interface{}{"action": "update", "task_id": 1, "status": "completed"},
			setup: func(m *mockListStore, ts ports.TaskStore) {
				m.tasks = []ports.Task{{ID: 1, Content: "task 1", Status: "pending"}}
				if s, ok := ts.(ports.Initializer); ok {
					_ = s.Initialize(context.Background())
				}
			},
			expectedResult: "Task 1 updated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pt, provider := setupPersistenceTools()
			setupManageTasks(t, tt.setup, provider)

			res, err := pt.ManageTasks(context.Background(), tt.args, nil)

			assertManageTasksResult(t, res, err, tt.expectedResult, false)
		})
	}
}

func testManageTasksDeleteAndClear(t *testing.T) {
	tests := []struct {
		name           string
		args           map[string]interface{}
		setup          func(*mockListStore, ports.TaskStore)
		expectedResult string
	}{
		{
			name: "delete task",
			args: map[string]interface{}{"action": "delete", "task_id": 1},
			setup: func(m *mockListStore, ts ports.TaskStore) {
				m.tasks = []ports.Task{{ID: 1, Content: "task 1", Status: "pending"}}
				if s, ok := ts.(ports.Initializer); ok {
					_ = s.Initialize(context.Background())
				}
			},
			expectedResult: "Task 1 deleted",
		},
		{
			name: "clear tasks",
			args: map[string]interface{}{"action": "clear"},
			setup: func(m *mockListStore, ts ports.TaskStore) {
				m.tasks = []ports.Task{{ID: 1, Content: "task 1", Status: "pending"}}
				if s, ok := ts.(ports.Initializer); ok {
					_ = s.Initialize(context.Background())
				}
			},
			expectedResult: "All tasks cleared",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pt, provider := setupPersistenceTools()
			setupManageTasks(t, tt.setup, provider)

			res, err := pt.ManageTasks(context.Background(), tt.args, nil)

			assertManageTasksResult(t, res, err, tt.expectedResult, false)
		})
	}
}

func testManageTasksCompletedFilter(t *testing.T) {
	tests := []struct {
		name           string
		args           map[string]interface{}
		setup          func(*mockListStore, ports.TaskStore)
		expectedResult string
	}{
		{
			name: "list completed",
			args: map[string]interface{}{"action": "list", "status": "completed"},
			setup: func(m *mockListStore, ts ports.TaskStore) {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pt, provider := setupPersistenceTools()
			setupManageTasks(t, tt.setup, provider)

			res, err := pt.ManageTasks(context.Background(), tt.args, nil)

			assertManageTasksResult(t, res, err, tt.expectedResult, false)
		})
	}
}

func testManageTasksError(t *testing.T) {
	tests := []struct {
		name           string
		args           map[string]interface{}
		expectedResult string
		expectError    bool
	}{
		{
			name:           "unknown action",
			args:           map[string]interface{}{"action": "unknown"},
			expectedResult: "Error: unknown action: unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pt, provider := setupPersistenceTools()
			setupManageTasks(t, nil, provider)

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

	_, err = pt.ManageTasks(ctx, map[string]interface{}{"action": "list"}, nil)
	if err == nil {
		t.Error("Expected error from listTasks via fetchAndCount")
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
	_, err = pt.ManageTasks(ctx, map[string]interface{}{"action": "update", "task_id": 999, "status": "completed"}, nil)
	if err == nil {
		t.Error("Expected error for non-existent task in updateTask")
	}

	// deleteTask error (not found)
	_, err = pt.ManageTasks(ctx, map[string]interface{}{"action": "delete", "task_id": 999}, nil)
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
		"offset": 50,
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

// TestManageTasks_SchemaTaskIDIsInteger verifies that the LLM-facing JSON
// schema for the manage_tasks tool declares task_id as "INTEGER", not
// "NUMBER". This prevents LLMs from emitting fractional IDs (e.g., 3.5)
// that would fail validation at the domain boundary (ports.Task.ID is int64).
// TestListTasks_FilteredNoMatch verifies the offset-beyond-range edge case:
// when tasks exist (totalCount > 0) but offset >= len(tasks) causes an empty
// result set, listTasks returns "No tasks found. (total: N)".
func TestListTasks_FilteredNoMatch(t *testing.T) {
	pt, provider := setupPersistenceTools()
	ctx := context.Background()

	_, err := provider.tasks.AddTask(ctx, "pending task 1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.tasks.AddTask(ctx, "pending task 2")
	if err != nil {
		t.Fatal(err)
	}

	// Offset >= total matching tasks → empty slice, but CountTasks still sees them
	res, err := pt.ManageTasks(ctx, map[string]interface{}{
		"action": "list",
		"offset": 10,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "No tasks found. (total: 2)") {
		t.Errorf("expected 'No tasks found. (total: 2)', got: %q", res.Text)
	}
}

func TestManageTasks_SchemaTaskIDIsInteger(t *testing.T) {
	pt, _ := setupPersistenceTools()
	reg := registry.New()
	if err := pt.Register(reg); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	var manageDecl *tools.ToolDeclaration
	for _, d := range reg.GetDeclarations() {
		if d.Name == "manage_tasks" {
			manageDecl = d
			break
		}
	}
	if manageDecl == nil {
		t.Fatal("manage_tasks not found in declarations")
	}
	if manageDecl.Parameters == nil {
		t.Fatal("manage_tasks has no parameters schema")
	}

	taskIDProp := manageDecl.Parameters.Properties["task_id"]
	if taskIDProp == nil {
		t.Fatal("task_id parameter not found in manage_tasks schema")
	}
	if taskIDProp.Type != "INTEGER" {
		t.Errorf("task_id schema type = %q; want %q", taskIDProp.Type, "INTEGER")
	}
}

func TestTaskIcon(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   string
	}{
		{"pending status", "pending", "[ ]"},
		{"completed status", "completed", "[x]"},
		{"empty status defaults to unchecked", "", "[ ]"},
		{"unknown status defaults to unchecked", "archived", "[ ]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := ports.Task{Status: tt.status}
			got := taskIcon(task)
			if got != tt.want {
				t.Errorf("taskIcon(%q) = %q; want %q", tt.status, got, tt.want)
			}
		})
	}
}

// assertRenderOutput validates renderTaskPage output against expected
// contains, exact-match, and not-contains criteria. The wantExact check
// short-circuits: when wantExact is non-empty, contains/not-contains
// checks are skipped.
func assertRenderOutput(t *testing.T, got string, wantContains []string, wantExact string, notContains []string) {
	t.Helper()

	if wantExact != "" {
		if got != wantExact {
			t.Errorf("renderTaskPage = %q; want exact %q", got, wantExact)
		}
		return
	}

	for _, want := range wantContains {
		if !strings.Contains(got, want) {
			t.Errorf("renderTaskPage missing %q in:\n%s", want, got)
		}
	}

	for _, notWant := range notContains {
		if strings.Contains(got, notWant) {
			t.Errorf("renderTaskPage unexpectedly contains %q in:\n%s", notWant, got)
		}
	}
}

func TestRenderTaskPage(t *testing.T) {
	tests := []struct {
		name         string
		tasks        []ports.Task
		totalCount   int
		offset       int
		limit        int
		wantContains []string
		wantExact    string
		notContains  []string
	}{
		{
			name:       "empty store zero total",
			tasks:      []ports.Task{},
			totalCount: 0,
			offset:     0,
			limit:      50,
			wantExact:  "No tasks found.",
		},
		{
			name:       "empty result non-zero total",
			tasks:      []ports.Task{},
			totalCount: 5,
			offset:     10,
			limit:      50,
			wantExact:  "No tasks found. (total: 5)",
		},
		{
			name: "single pending task",
			tasks: []ports.Task{
				{ID: 1, Content: "write tests", Status: "pending"},
			},
			totalCount: 1,
			offset:     0,
			limit:      50,
			wantContains: []string{
				"Tasks (showing 1-1 of 1):",
				"1. [ ] write tests (pending)",
			},
		},
		{
			name: "single completed task",
			tasks: []ports.Task{
				{ID: 42, Content: "ship it", Status: "completed"},
			},
			totalCount: 1,
			offset:     0,
			limit:      50,
			wantContains: []string{
				"Tasks (showing 1-1 of 1):",
				"42. [x] ship it (completed)",
			},
		},
		{
			name: "mixed statuses",
			tasks: []ports.Task{
				{ID: 1, Content: "pending task", Status: "pending"},
				{ID: 2, Content: "done task", Status: "completed"},
			},
			totalCount: 2,
			offset:     0,
			limit:      50,
			wantContains: []string{
				"Tasks (showing 1-2 of 2):",
				"[ ] pending task",
				"[x] done task",
			},
		},
		{
			name: "pagination hint present",
			tasks: func() []ports.Task {
				out := make([]ports.Task, 50)
				for i := 0; i < 50; i++ {
					out[i] = ports.Task{ID: int64(i + 1), Content: fmt.Sprintf("task %d", i+1), Status: "pending"}
				}
				return out
			}(),
			totalCount: 60,
			offset:     0,
			limit:      50,
			wantContains: []string{
				"Tasks (showing 1-50 of 60):",
				"\nUse offset=50 for next page.",
			},
		},
		{
			name: "last page no hint",
			tasks: func() []ports.Task {
				out := make([]ports.Task, 10)
				for i := 0; i < 10; i++ {
					out[i] = ports.Task{ID: int64(i + 51), Content: fmt.Sprintf("task %d", i+51), Status: "pending"}
				}
				return out
			}(),
			totalCount: 60,
			offset:     50,
			limit:      50,
			wantContains: []string{
				"Tasks (showing 51-60 of 60):",
			},
			notContains: []string{
				"Use offset=",
			},
		},
		{
			name: "partial last page correct range",
			tasks: func() []ports.Task {
				out := make([]ports.Task, 5)
				for i := 0; i < 5; i++ {
					out[i] = ports.Task{ID: int64(i + 51), Content: fmt.Sprintf("task %d", i+51), Status: "pending"}
				}
				return out
			}(),
			totalCount: 55,
			offset:     50,
			limit:      50,
			wantContains: []string{
				"Tasks (showing 51-55 of 55):",
			},
			notContains: []string{
				"Use offset=",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderTaskPage(tt.tasks, tt.totalCount, tt.offset, tt.limit)
			assertRenderOutput(t, got, tt.wantContains, tt.wantExact, tt.notContains)
		})
	}
}

// countErrorStore wraps a ports.TaskStore and overrides CountTasks to
// return a configurable error, while delegating all other methods (including
// ListTasks) to the inner store. This enables testing the CountTasks error
// path independently from ListTasks.
type countErrorStore struct {
	ports.TaskStore
	countErr error
}

func (s *countErrorStore) CountTasks(ctx context.Context, status string) (int, error) {
	if s.countErr != nil {
		return 0, s.countErr
	}
	return s.TaskStore.CountTasks(ctx, status)
}

// TestFetchAndCount_Success verifies the happy path: tasks and count
// are both returned correctly.
func TestFetchAndCount_Success(t *testing.T) {
	t.Parallel()
	pt, provider := setupPersistenceTools()
	ctx := context.Background()

	_, err := provider.tasks.AddTask(ctx, "task one")
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.tasks.AddTask(ctx, "task two")
	if err != nil {
		t.Fatal(err)
	}

	tasks, count, err := pt.fetchAndCount(ctx, "", 50, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d; want 2", count)
	}
	if len(tasks) != 2 {
		t.Errorf("len(tasks) = %d; want 2", len(tasks))
	}
}

// TestFetchAndCount_ListTasksError verifies that a ListTasks error
// propagates correctly, returning nil tasks and zero count.
func TestFetchAndCount_ListTasksError(t *testing.T) {
	t.Parallel()
	pt, provider := setupPersistenceTools()
	provider.listStore.err = errors.New("list failed")

	tasks, count, err := pt.fetchAndCount(context.Background(), "", 50, 0)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "list failed") {
		t.Errorf("error = %q; want containing 'list failed'", err.Error())
	}
	if tasks != nil {
		t.Errorf("tasks = %v; want nil on error", tasks)
	}
	if count != 0 {
		t.Errorf("count = %d; want 0 on error", count)
	}
}

// TestFetchAndCount_CountTasksError verifies that when ListTasks succeeds
// but CountTasks fails, the error propagates correctly. Uses countErrorStore
// to independently control the CountTasks error without affecting ListTasks.
func TestFetchAndCount_CountTasksError(t *testing.T) {
	t.Parallel()
	pt, provider := setupPersistenceTools()
	ctx := context.Background()

	// Add a task so ListTasks succeeds
	_, err := provider.tasks.AddTask(ctx, "task one")
	if err != nil {
		t.Fatal(err)
	}

	// Wrap the real TaskStore so ListTasks works but CountTasks fails
	pt.tasks = &countErrorStore{
		TaskStore: provider.tasks,
		countErr:  errors.New("count failed"),
	}

	tasks, count, err := pt.fetchAndCount(ctx, "", 50, 0)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "count failed") {
		t.Errorf("error = %q; want containing 'count failed'", err.Error())
	}
	if tasks != nil {
		t.Errorf("tasks = %v; want nil on error", tasks)
	}
	if count != 0 {
		t.Errorf("count = %d; want 0 on error", count)
	}
}

// TestRetryOnBusy_ContextCancellation verifies that when the context is
// cancelled before the first retry, retryOnBusy returns ctx.Err() without
// executing further retries.
//
// Uses a pre-cancelled context per ADR-036 §2 (no time.Sleep).
func TestRetryOnBusy_ContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled — ADR-036 §2 pattern

	op := busyFunc(99, nil) // always returns BUSY; 99 ensures exhaustion is impossible

	err := retryOnBusy(ctx, op)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

// TestRetryOnBusy_Exhaustion verifies that after 3 consecutive BUSY errors,
// retryOnBusy returns the last error and the operation was called exactly 3 times.
func TestRetryOnBusy_Exhaustion(t *testing.T) {
	t.Parallel()

	calls := 0
	op := func() error {
		calls++
		return fmt.Errorf("database is locked (SQLITE_BUSY)")
	}

	err := retryOnBusy(context.Background(), op)
	if err == nil {
		t.Fatal("expected error after retry exhaustion, got nil")
	}
	if !strings.Contains(err.Error(), "database is locked") {
		t.Errorf("expected BUSY error, got: %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls after exhaustion, got %d", calls)
	}
}

// TestRetryOnBusy_SuccessAfterRetry verifies that retryOnBusy retries
// after BUSY errors with exponential backoff (100ms → 200ms) and
// eventually succeeds. The time.After backoff path (lines 70-71) is
// exercised because the operation returns BUSY twice before succeeding.
func TestRetryOnBusy_SuccessAfterRetry(t *testing.T) {
	t.Parallel()

	calls := 0
	op := func() error {
		calls++
		if calls <= 2 {
			return fmt.Errorf("database is locked (SQLITE_BUSY)")
		}
		return nil
	}

	start := time.Now()
	err := retryOnBusy(context.Background(), op)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls (2 BUSY + 1 success), got %d", calls)
	}
	if elapsed < 250*time.Millisecond {
		t.Errorf("expected backoff delay >= 250ms (100ms + 200ms), got %v", elapsed)
	}
}
