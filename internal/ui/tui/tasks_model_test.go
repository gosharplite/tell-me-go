// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// ── TaskStore mock ──

type mockTaskStore struct {
	ListTasksFunc  func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error)
	CountTasksFunc func(ctx context.Context, status string) (int, error)

	// TaskWriter methods (unused by taskListModel but needed for interface)
	AddTaskFunc    func(ctx context.Context, content string) (ports.Task, error)
	UpdateTaskFunc func(ctx context.Context, id int64, content, status string) (ports.Task, error)
	DeleteTaskFunc func(ctx context.Context, id int64) error
	ClearTasksFunc func(ctx context.Context) error
}

func (m *mockTaskStore) ListTasks(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) {
	if m.ListTasksFunc != nil {
		return m.ListTasksFunc(ctx, status, limit, offset)
	}
	return nil, nil
}

func (m *mockTaskStore) CountTasks(ctx context.Context, status string) (int, error) {
	if m.CountTasksFunc != nil {
		return m.CountTasksFunc(ctx, status)
	}
	return 0, nil
}

func (m *mockTaskStore) AddTask(ctx context.Context, content string) (ports.Task, error) {
	if m.AddTaskFunc != nil {
		return m.AddTaskFunc(ctx, content)
	}
	return ports.Task{}, nil
}

func (m *mockTaskStore) UpdateTask(ctx context.Context, id int64, content, status string) (ports.Task, error) {
	if m.UpdateTaskFunc != nil {
		return m.UpdateTaskFunc(ctx, id, content, status)
	}
	return ports.Task{}, nil
}

func (m *mockTaskStore) DeleteTask(ctx context.Context, id int64) error {
	if m.DeleteTaskFunc != nil {
		return m.DeleteTaskFunc(ctx, id)
	}
	return nil
}

func (m *mockTaskStore) ClearTasks(ctx context.Context) error {
	if m.ClearTasksFunc != nil {
		return m.ClearTasksFunc(ctx)
	}
	return nil
}

// ── normalizeStatusFilter tests ──

func TestNormalizeStatusFilter(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"exact pending", "pending", "pending"},
		{"exact completed", "completed", "completed"},
		{"pending with spaces", "  pending  ", "pending"},
		{"completed uppercase", "COMPLETED", "completed"},
		{"completed mixed case", "Completed", "completed"},
		{"arbitrary text", "task 1", ""},
		{"empty string", "", ""},
		{"whitespace only", "   ", ""},
		{"status substring", "pend", ""},
		{"status with extra chars", "pending!", ""},
		{"completion typo", "complete", ""},
		{"partial word with space", " pending ", "pending"},
		{"trailing newline", "completed\n", "completed"},
		{"leading tab", "\tpending", "pending"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeStatusFilter(tt.input)
			if got != tt.want {
				t.Errorf("normalizeStatusFilter(%q) = %q; want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ── taskListModel tests ──

func TestNewTaskListModel_PlaceholderAndPrompt(t *testing.T) {
	m := newTaskListModel(context.Background(), &mockTaskStore{})

	if m.searchBar.Placeholder != "Status filter (pending/completed)..." {
		t.Errorf("placeholder = %q; want %q",
			m.searchBar.Placeholder, "Status filter (pending/completed)...")
	}
	if m.searchBar.Prompt != "📋 " {
		t.Errorf("prompt = %q; want %q", m.searchBar.Prompt, "📋 ")
	}
}

func TestTaskListModel_SearchEnter_NormalizesStatus(t *testing.T) {
	// Track what status ListTasks and CountTasks receive
	var receivedStatus string

	store := &mockTaskStore{
		ListTasksFunc: func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) {
			receivedStatus = status
			return []ports.Task{{ID: 1, Content: "test", Status: "pending"}}, nil
		},
		CountTasksFunc: func(ctx context.Context, status string) (int, error) {
			return 1, nil
		},
	}

	m := newTaskListModel(context.Background(), store)
	m.searchBar.Focus()
	m.searchBar.SetValue("task 1") // arbitrary text

	// Simulate pressing enter
	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_ = newModel

	// Execute the returned command to trigger the fetch
	if cmd == nil {
		t.Fatal("expected non-nil cmd from enter key")
	}
	msg := cmd()
	tasksMsg, ok := msg.(tasksLoadedMsg)
	if !ok {
		t.Fatalf("expected tasksLoadedMsg, got %T", msg)
	}
	if tasksMsg.err != nil {
		t.Fatalf("unexpected error: %v", tasksMsg.err)
	}

	// The status passed to ListTasks should be "" (empty = all), not "task 1"
	if receivedStatus != "" {
		t.Errorf("expected empty status for arbitrary input, got %q", receivedStatus)
	}
	// Should still get results (1 task returned)
	if len(tasksMsg.tasks) != 1 {
		t.Errorf("expected 1 task, got %d", len(tasksMsg.tasks))
	}
}

func TestTaskListModel_SearchEnter_ValidStatus_Pending(t *testing.T) {
	var receivedStatus string

	store := &mockTaskStore{
		ListTasksFunc: func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) {
			receivedStatus = status
			return []ports.Task{}, nil
		},
		CountTasksFunc: func(ctx context.Context, status string) (int, error) { return 0, nil },
	}

	m := newTaskListModel(context.Background(), store)
	m.searchBar.Focus()
	m.searchBar.SetValue("pending")

	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_ = newModel

	if cmd == nil {
		t.Fatal("expected non-nil cmd from enter key")
	}
	cmd() // execute fetch

	if receivedStatus != "pending" {
		t.Errorf("expected status 'pending', got %q", receivedStatus)
	}
}

func TestTaskListModel_SearchEnter_ValidStatus_Completed(t *testing.T) {
	var receivedStatus string

	store := &mockTaskStore{
		ListTasksFunc: func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) {
			receivedStatus = status
			return []ports.Task{}, nil
		},
		CountTasksFunc: func(ctx context.Context, status string) (int, error) { return 0, nil },
	}

	m := newTaskListModel(context.Background(), store)
	m.searchBar.Focus()
	m.searchBar.SetValue("  COMPLETED  ")

	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_ = newModel

	if cmd == nil {
		t.Fatal("expected non-nil cmd from enter key")
	}
	cmd()

	if receivedStatus != "completed" {
		t.Errorf("expected status 'completed', got %q", receivedStatus)
	}
}

func TestTaskListModel_SearchEsc_ResetsFilter(t *testing.T) {
	var receivedStatus string

	store := &mockTaskStore{
		ListTasksFunc: func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) {
			receivedStatus = status
			return []ports.Task{}, nil
		},
		CountTasksFunc: func(ctx context.Context, status string) (int, error) { return 0, nil },
	}

	m := newTaskListModel(context.Background(), store)
	m.searchBar.Focus()
	m.searchBar.SetValue("pending")

	// Press esc
	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	_ = newModel

	if cmd == nil {
		t.Fatal("expected non-nil cmd from esc key")
	}
	cmd()

	if receivedStatus != "" {
		t.Errorf("expected empty status after esc (show all), got %q", receivedStatus)
	}

	updated := newModel.(*taskListModel)
	if updated.searchBar.Value() != "" {
		t.Errorf("expected search bar to be cleared after esc, got %q", updated.searchBar.Value())
	}
}

func TestTaskListModel_SearchEnter_BlurAndReset(t *testing.T) {
	store := &mockTaskStore{
		ListTasksFunc:  func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) { return nil, nil },
		CountTasksFunc: func(ctx context.Context, status string) (int, error) { return 0, nil },
	}

	m := newTaskListModel(context.Background(), store)
	m.searchBar.Focus()
	m.selected = 3
	m.pageOffset = 10

	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := newModel.(*taskListModel)

	if updated.searchBar.Focused() {
		t.Error("expected search bar to be blurred after enter")
	}
	if updated.pageOffset != 0 {
		t.Errorf("expected pageOffset 0, got %d", updated.pageOffset)
	}
	if updated.selected != -1 {
		t.Errorf("expected selected -1, got %d", updated.selected)
	}
}

func TestTaskListModel_SearchBar_FocusAndUnfocus(t *testing.T) {
	store := &mockTaskStore{
		ListTasksFunc:  func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) { return nil, nil },
		CountTasksFunc: func(ctx context.Context, status string) (int, error) { return 0, nil },
	}

	m := newTaskListModel(context.Background(), store)

	// Focus search bar with "/"
	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	updated := newModel.(*taskListModel)
	if !updated.searchBar.Focused() {
		t.Error("expected search bar to be focused after '/'")
	}

	// Type some text while focused
	updated.searchBar.SetValue("hello")
	newModel2, _ := updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("pending")})
	_ = newModel2
	// Value may have changed due to textinput handling — just verify no panic
}

func TestTaskListModel_FooterText(t *testing.T) {
	store := &mockTaskStore{
		ListTasksFunc:  func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) { return nil, nil },
		CountTasksFunc: func(ctx context.Context, status string) (int, error) { return 0, nil },
	}

	m := newTaskListModel(context.Background(), store)
	footer := m.renderFooter()

	// Footer should mention filter
	if !strings.Contains(footer, "filter") {
		t.Errorf("expected footer to mention 'filter', got %q", footer)
	}
}

func TestTaskListModel_MoveSelection_Bounds(t *testing.T) {
	m := &taskListModel{
		tasks: []ports.Task{
			{ID: 1, Content: "task 1", Status: "pending"},
			{ID: 2, Content: "task 2", Status: "completed"},
		},
		selected: 0,
	}

	m.moveSelection(-5)
	if m.selected != 0 {
		t.Errorf("expected selected 0 (clamped), got %d", m.selected)
	}

	m.moveSelection(10)
	if m.selected != 1 {
		t.Errorf("expected selected 1 (clamped), got %d", m.selected)
	}

	// Empty tasks
	m.tasks = nil
	m.selected = 5
	m.moveSelection(1)
	if m.selected != 5 {
		t.Errorf("expected selected unchanged for empty tasks, got %d", m.selected)
	}
}

func TestTaskListModel_View_ErrorState(t *testing.T) {
	m := &taskListModel{
		err:   context.DeadlineExceeded,
		ready: true,
	}
	view := m.View()
	if !strings.Contains(view, "Error:") {
		t.Errorf("expected error in view, got %q", view)
	}
}

func TestTaskListModel_HandleWindowSizeMsg_Bounds(t *testing.T) {
	tests := []struct {
		name          string
		msg           tea.WindowSizeMsg
		ready         bool
		wantWidth     int
		wantHeight    int
		wantViewportH int // expected viewport.Height after handling
		wantReady     bool
	}{
		{
			name:          "normal dimensions",
			msg:           tea.WindowSizeMsg{Width: 100, Height: 40},
			ready:         false,
			wantWidth:     100,
			wantHeight:    40,
			wantViewportH: 39, // 40 - 1 footer
			wantReady:     true,
		},
		{
			name:          "minimum width clamped",
			msg:           tea.WindowSizeMsg{Width: 5, Height: 40},
			ready:         false,
			wantWidth:     20, // clamped
			wantHeight:    40,
			wantViewportH: 39,
			wantReady:     true,
		},
		{
			name:          "minimum height clamped",
			msg:           tea.WindowSizeMsg{Width: 80, Height: 2},
			ready:         false,
			wantWidth:     80,
			wantHeight:    5, // clamped
			wantViewportH: 4, // 5 - 1
			wantReady:     true,
		},
		{
			name:          "viewport height floor at 3",
			msg:           tea.WindowSizeMsg{Width: 80, Height: 4},
			ready:         false,
			wantWidth:     80,
			wantHeight:    5, // clamped to 5 (below that would give vpHeight <3)
			wantViewportH: 4,
			wantReady:     true,
		},
		{
			name:          "zero dimensions clamped",
			msg:           tea.WindowSizeMsg{Width: 0, Height: 0},
			ready:         false,
			wantWidth:     20, // clamped
			wantHeight:    5,  // clamped
			wantViewportH: 4,  // 5 - 1
			wantReady:     true,
		},
		{
			name:          "already ready resizes viewport",
			msg:           tea.WindowSizeMsg{Width: 120, Height: 50},
			ready:         true,
			wantWidth:     120,
			wantHeight:    50,
			wantViewportH: 49,
			wantReady:     true,
		},
		{
			name:          "already ready with small dimensions",
			msg:           tea.WindowSizeMsg{Width: 3, Height: 1},
			ready:         true,
			wantWidth:     20, // clamped
			wantHeight:    5,  // clamped
			wantViewportH: 4,
			wantReady:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &taskListModel{
				ready:  tt.ready,
				tasks:  []ports.Task{},
				width:  0,
				height: 0,
			}
			if tt.ready {
				// Pre-create a viewport so the "already ready" branch is exercised
				m.viewport = viewport.New(80, 24)
				m.width = 80
				m.height = 24
			}

			newModel, cmd := m.handleWindowSizeMsg(tt.msg)
			updated := newModel.(*taskListModel)

			if updated.width != tt.wantWidth {
				t.Errorf("width = %d, want %d", updated.width, tt.wantWidth)
			}
			if updated.height != tt.wantHeight {
				t.Errorf("height = %d, want %d", updated.height, tt.wantHeight)
			}
			if updated.ready != tt.wantReady {
				t.Errorf("ready = %v, want %v", updated.ready, tt.wantReady)
			}
			if updated.viewport.Height != tt.wantViewportH {
				t.Errorf("viewport.Height = %d, want %d", updated.viewport.Height, tt.wantViewportH)
			}

			// cmd may be nil — WindowSizeMsg does not trigger a viewport command
			_ = cmd
		})
	}
}

func TestTaskListModel_View_NotReady(t *testing.T) {
	m := &taskListModel{ready: false}
	view := m.View()
	if !strings.Contains(view, "Initializing") {
		t.Errorf("expected 'Initializing' in view, got %q", view)
	}
}

func TestTaskListModel_View_WithSearchFocused(t *testing.T) {
	store := &mockTaskStore{
		ListTasksFunc:  func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) { return nil, nil },
		CountTasksFunc: func(ctx context.Context, status string) (int, error) { return 0, nil },
	}

	m := newTaskListModel(context.Background(), store)
	m.ready = true
	m.searchBar.Focus()
	m.searchBar.SetValue("pending")

	view := m.View()
	// Search bar view should be visible (rendered by footerStyle in focused state)
	if !strings.Contains(view, "pending") {
		t.Errorf("expected search bar value in view, got %q", view)
	}
}

func TestTaskListModel_RenderTasks_Empty(t *testing.T) {
	m := &taskListModel{}
	got := m.renderTasks()
	if got != "No tasks." {
		t.Errorf("expected 'No tasks.', got %q", got)
	}
}

func TestTaskListModel_RenderTasks_WithTasks(t *testing.T) {
	m := &taskListModel{
		tasks: []ports.Task{
			{ID: 1, Content: "first", Status: "pending"},
			{ID: 2, Content: "second", Status: "completed"},
		},
		selected: 1,
	}

	got := m.renderTasks()

	// Selected task should have "> " prefix
	if !strings.Contains(got, "> ") {
		t.Errorf("expected selected indicator '> ', got %q", got)
	}
	// Unselected should have "  " prefix
	if !strings.Contains(got, "  ") {
		t.Errorf("expected unselected indicator '  ', got %q", got)
	}
	// Completed task should show [x]
	if !strings.Contains(got, "[x]") {
		t.Errorf("expected completed indicator '[x]', got %q", got)
	}
	// Pending task should show [ ]
	if !strings.Contains(got, "[ ]") {
		t.Errorf("expected pending indicator '[ ]', got %q", got)
	}
}

func TestTaskListModel_UpdateViewportHeight_NotReady(t *testing.T) {
	m := &taskListModel{ready: false}
	m.updateViewportHeight()
	// Should not panic and should not change height
	if m.viewport.Height != 0 {
		t.Errorf("expected viewport height 0, got %d", m.viewport.Height)
	}
}

func TestTaskListModel_Init(t *testing.T) {
	store := &mockTaskStore{
		ListTasksFunc:  func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) { return nil, nil },
		CountTasksFunc: func(ctx context.Context, status string) (int, error) { return 0, nil },
	}
	m := newTaskListModel(context.Background(), store)
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected Init to return a command")
	}
}

func TestTaskListModel_HandleTasksLoadedMsg_Error(t *testing.T) {
	m := &taskListModel{}
	newModel, cmd := m.handleTasksLoadedMsg(tasksLoadedMsg{
		err: context.DeadlineExceeded,
	})
	updated := newModel.(*taskListModel)
	if updated.err == nil {
		t.Fatal("expected error to be set")
	}
	if cmd != nil {
		t.Error("expected nil cmd on error")
	}
}

func TestTaskListModel_HandleTasksLoadedMsg_Success(t *testing.T) {
	m := &taskListModel{selected: -1}
	tasks := []ports.Task{
		{ID: 1, Content: "task 1", Status: "pending"},
	}
	newModel, _ := m.handleTasksLoadedMsg(tasksLoadedMsg{
		tasks:      tasks,
		totalCount: 42,
	})
	updated := newModel.(*taskListModel)

	if len(updated.tasks) != 1 {
		t.Errorf("expected 1 task, got %d", len(updated.tasks))
	}
	if updated.totalCount != 42 {
		t.Errorf("expected totalCount 42, got %d", updated.totalCount)
	}
	if updated.selected != 0 {
		t.Errorf("expected selected 0 (auto-select on initial load), got %d", updated.selected)
	}
}

func TestTaskListModel_QuitKeys(t *testing.T) {
	store := &mockTaskStore{
		ListTasksFunc:  func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) { return nil, nil },
		CountTasksFunc: func(ctx context.Context, status string) (int, error) { return 0, nil },
	}

	tests := []struct {
		name string
		key  tea.KeyMsg
	}{
		{"q quits", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}},
		{"esc quits", tea.KeyMsg{Type: tea.KeyEsc}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTaskListModel(context.Background(), store)
			_, cmd := m.Update(tt.key)
			if cmd == nil {
				t.Fatal("expected quit command")
			}
		})
	}
}

func TestTaskListModel_FetchTasksCmd(t *testing.T) {
	store := &mockTaskStore{
		ListTasksFunc: func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) {
			return []ports.Task{
				{ID: 1, Content: "test", Status: status},
			}, nil
		},
		CountTasksFunc: func(ctx context.Context, status string) (int, error) {
			if status == "pending" {
				return 5, nil
			}
			return 10, nil
		},
	}

	cmd := fetchTasksCmd(context.Background(), store, "pending", 0, 50)
	msg := cmd()
	tasksMsg, ok := msg.(tasksLoadedMsg)
	if !ok {
		t.Fatalf("expected tasksLoadedMsg, got %T", msg)
	}
	if tasksMsg.err != nil {
		t.Fatalf("unexpected error: %v", tasksMsg.err)
	}
	if len(tasksMsg.tasks) != 1 {
		t.Errorf("expected 1 task, got %d", len(tasksMsg.tasks))
	}
	if tasksMsg.totalCount != 5 {
		t.Errorf("expected totalCount 5, got %d", tasksMsg.totalCount)
	}
}

// ── Page navigation tests ──

// assertPageNav verifies the full page-navigation lifecycle:
// 1. Press key → pendingPageOffset set, pageOffset unchanged
// 2. Execute fetch → correct offset sent to ListTasks
// 3. Feed success → pageOffset advanced, pending cleared
// 4. Selection reset to 0
func assertPageNav(t *testing.T, m *taskListModel, key tea.KeyMsg, wantPendingOffset, wantListOffset int, totalCount int) {
	t.Helper()

	newModel, cmd := m.Update(key)
	if cmd == nil {
		t.Fatal("expected non-nil cmd from page nav key")
	}

	updated := newModel.(*taskListModel)
	if updated.pendingPageOffset != wantPendingOffset {
		t.Errorf("pendingPageOffset = %d, want %d", updated.pendingPageOffset, wantPendingOffset)
	}
	if !updated.pageNavPending {
		t.Error("expected pageNavPending to be true")
	}

	// Execute the fetch command
	msg := cmd()
	tasksMsg, ok := msg.(tasksLoadedMsg)
	if !ok {
		t.Fatalf("expected tasksLoadedMsg, got %T", msg)
	}
	if tasksMsg.err != nil {
		t.Fatalf("unexpected fetch error: %v", tasksMsg.err)
	}

	// Feed the success message back
	newModel2, _ := updated.Update(tasksMsg)
	updated2 := newModel2.(*taskListModel)

	if updated2.pageOffset != wantPendingOffset {
		t.Errorf("pageOffset = %d, want %d after successful fetch", updated2.pageOffset, wantPendingOffset)
	}
	if updated2.pendingPageOffset != 0 {
		t.Errorf("pendingPageOffset = %d, want 0 after fetch", updated2.pendingPageOffset)
	}
	if updated2.pageNavPending {
		t.Error("expected pageNavPending to be false after fetch")
	}
	if updated2.selected != 0 {
		t.Errorf("expected selected reset to 0, got %d", updated2.selected)
	}
}

func TestTaskListModel_PageNav_NextPage_IncrementsOffset(t *testing.T) {
	var receivedOffset int
	store := &mockTaskStore{
		ListTasksFunc: func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) {
			receivedOffset = offset
			tasks := make([]ports.Task, 50)
			for i := range tasks {
				tasks[i] = ports.Task{ID: int64(offset + i + 1), Content: "task", Status: "pending"}
			}
			return tasks, nil
		},
		CountTasksFunc: func(ctx context.Context, status string) (int, error) { return 100, nil },
	}

	m := newTaskListModel(context.Background(), store)
	m.ready = true
	m.tasks = make([]ports.Task, 50)
	m.totalCount = 100
	m.pageOffset = 0
	m.selected = 0
	m.pageSize = 50

	assertPageNav(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}, 50, 50, 100)

	if receivedOffset != 50 {
		t.Errorf("expected offset 50 passed to ListTasks, got %d", receivedOffset)
	}
}

func TestTaskListModel_PageNav_PrevPage_DecrementsOffset(t *testing.T) {
	var receivedOffset int
	store := &mockTaskStore{
		ListTasksFunc: func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) {
			receivedOffset = offset
			tasks := make([]ports.Task, 50)
			for i := range tasks {
				tasks[i] = ports.Task{ID: int64(offset + i + 1), Content: "task", Status: "pending"}
			}
			return tasks, nil
		},
		CountTasksFunc: func(ctx context.Context, status string) (int, error) { return 100, nil },
	}

	m := newTaskListModel(context.Background(), store)
	m.ready = true
	m.tasks = make([]ports.Task, 50)
	m.totalCount = 100
	m.pageOffset = 50
	m.selected = 0
	m.pageSize = 50

	assertPageNav(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")}, 0, 0, 100)

	if receivedOffset != 0 {
		t.Errorf("expected offset 0 passed to ListTasks, got %d", receivedOffset)
	}
}

func TestTaskListModel_PageNav_PrevPage_AtZero_Clamped(t *testing.T) {
	var receivedOffset int

	store := &mockTaskStore{
		ListTasksFunc: func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) {
			receivedOffset = offset
			return []ports.Task{}, nil
		},
		CountTasksFunc: func(ctx context.Context, status string) (int, error) {
			return 10, nil
		},
	}

	m := newTaskListModel(context.Background(), store)
	m.ready = true
	m.tasks = []ports.Task{}
	m.totalCount = 10
	m.pageOffset = 0
	m.selected = 0
	m.pageSize = 50

	// Press 'p' at page 0 — should clamp pendingPageOffset to 0
	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	_ = newModel

	if cmd == nil {
		t.Fatal("expected non-nil cmd from 'p' key")
	}

	updated := newModel.(*taskListModel)
	if updated.pendingPageOffset != 0 {
		t.Errorf("pendingPageOffset should be 0 (clamped), got %d", updated.pendingPageOffset)
	}
	if !updated.pageNavPending {
		t.Error("expected pageNavPending to be true")
	}

	cmd()

	if receivedOffset != 0 {
		t.Errorf("expected offset 0 (clamped), got %d", receivedOffset)
	}
}

func TestTaskListModel_PageNav_NextPage_AtLastPage_Clamped(t *testing.T) {
	var receivedOffset int

	store := &mockTaskStore{
		ListTasksFunc: func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) {
			receivedOffset = offset
			return []ports.Task{}, nil
		},
		CountTasksFunc: func(ctx context.Context, status string) (int, error) {
			return 100, nil
		},
	}

	m := newTaskListModel(context.Background(), store)
	m.ready = true
	m.tasks = make([]ports.Task, 50)
	m.totalCount = 100
	m.pageOffset = 50 // last page (50 + 50 >= 100)
	m.selected = 0
	m.pageSize = 50

	// Press 'n' at last page — should clamp to current offset
	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	_ = newModel

	if cmd == nil {
		t.Fatal("expected non-nil cmd from 'n' key")
	}

	updated := newModel.(*taskListModel)
	if updated.pendingPageOffset != 50 {
		t.Errorf("pendingPageOffset should be 50 (clamped at current page), got %d", updated.pendingPageOffset)
	}
	if !updated.pageNavPending {
		t.Error("expected pageNavPending to be true")
	}

	cmd()

	if receivedOffset != 50 {
		t.Errorf("expected offset 50 (clamped at last page), got %d", receivedOffset)
	}
}

func TestTaskListModel_Footer_ShowsCorrectRange(t *testing.T) {
	store := &mockTaskStore{
		ListTasksFunc:  func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) { return nil, nil },
		CountTasksFunc: func(ctx context.Context, status string) (int, error) { return 0, nil },
	}

	m := newTaskListModel(context.Background(), store)
	m.ready = true
	m.tasks = make([]ports.Task, 50)
	m.totalCount = 100
	m.pageOffset = 50
	m.pageSize = 50

	footer := m.renderFooter()

	if !strings.Contains(footer, "51-100 of 100") {
		t.Errorf("expected footer '51-100 of 100', got %q", footer)
	}
	if !strings.Contains(footer, "n/p") {
		t.Errorf("expected footer to mention 'n/p' page navigation keys, got %q", footer)
	}
}

func TestTaskListModel_Footer_AtPageZero(t *testing.T) {
	store := &mockTaskStore{
		ListTasksFunc:  func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) { return nil, nil },
		CountTasksFunc: func(ctx context.Context, status string) (int, error) { return 0, nil },
	}

	m := newTaskListModel(context.Background(), store)
	m.ready = true
	m.tasks = make([]ports.Task, 10)
	m.totalCount = 10
	m.pageOffset = 0
	m.pageSize = 50

	footer := m.renderFooter()

	if !strings.Contains(footer, "1-10 of 10") {
		t.Errorf("expected footer '1-10 of 10', got %q", footer)
	}
}

func TestTaskListModel_PageNav_PreservesStatusFilter(t *testing.T) {
	var receivedStatus string
	var receivedOffset int

	store := &mockTaskStore{
		ListTasksFunc: func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) {
			receivedStatus = status
			receivedOffset = offset
			return []ports.Task{{ID: 1, Content: "test", Status: "pending"}}, nil
		},
		CountTasksFunc: func(ctx context.Context, status string) (int, error) {
			return 100, nil
		},
	}

	m := newTaskListModel(context.Background(), store)
	m.ready = true
	m.statusFilter = "pending"
	m.tasks = []ports.Task{{ID: 1, Content: "test", Status: "pending"}}
	m.totalCount = 100
	m.pageOffset = 0
	m.selected = 0
	m.pageSize = 50

	// Press 'n' — should re-fetch with statusFilter="pending" and offset=50
	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	_ = newModel

	if cmd == nil {
		t.Fatal("expected non-nil cmd from 'n' key")
	}
	cmd()

	if receivedStatus != "pending" {
		t.Errorf("expected status 'pending', got %q", receivedStatus)
	}
	if receivedOffset != 50 {
		t.Errorf("expected offset 50, got %d", receivedOffset)
	}

	updated := newModel.(*taskListModel)
	if updated.pageOffset != 0 {
		t.Errorf("pageOffset should still be 0 before successful fetch, got %d", updated.pageOffset)
	}
	if updated.pendingPageOffset != 50 {
		t.Errorf("pendingPageOffset should be 50, got %d", updated.pendingPageOffset)
	}
}

func TestTaskListModel_PageNav_OffsetAppliedOnlyOnSuccess(t *testing.T) {
	store := &mockTaskStore{
		ListTasksFunc: func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) {
			return []ports.Task{
				{ID: int64(offset + 1), Content: "task", Status: "pending"},
			}, nil
		},
		CountTasksFunc: func(ctx context.Context, status string) (int, error) {
			return 100, nil
		},
	}

	m := newTaskListModel(context.Background(), store)
	m.ready = true
	m.tasks = []ports.Task{{ID: 1, Content: "page0", Status: "pending"}}
	m.totalCount = 100
	m.pageOffset = 0
	m.selected = 0
	m.pageSize = 50

	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	_ = newModel

	if cmd == nil {
		t.Fatal("expected non-nil cmd from 'n' key")
	}

	updated := newModel.(*taskListModel)
	if updated.pageOffset != 0 {
		t.Errorf("pageOffset should still be 0 before fetch completes, got %d", updated.pageOffset)
	}
	if updated.pendingPageOffset != 50 {
		t.Errorf("pendingPageOffset should be 50, got %d", updated.pendingPageOffset)
	}

	msg := cmd()
	tasksMsg := msg.(tasksLoadedMsg)
	if tasksMsg.err != nil {
		t.Fatalf("unexpected fetch error: %v", tasksMsg.err)
	}

	newModel2, _ := updated.Update(tasksMsg)
	updated2 := newModel2.(*taskListModel)

	if updated2.pageOffset != 50 {
		t.Errorf("pageOffset should be 50 after successful fetch, got %d", updated2.pageOffset)
	}
	if updated2.pendingPageOffset != 0 {
		t.Errorf("pendingPageOffset should be cleared to 0, got %d", updated2.pendingPageOffset)
	}
}

func TestTaskListModel_PageNav_ErrorDoesNotAdvanceOffset(t *testing.T) {
	store := &mockTaskStore{
		ListTasksFunc: func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) {
			return nil, nil
		},
		CountTasksFunc: func(ctx context.Context, status string) (int, error) {
			return 100, nil
		},
	}

	m := newTaskListModel(context.Background(), store)
	m.ready = true
	m.tasks = []ports.Task{{ID: 1, Content: "original", Status: "pending"}}
	m.totalCount = 100
	m.pageOffset = 0
	m.selected = 0
	m.pageSize = 50

	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	_ = cmd
	updated := newModel.(*taskListModel)

	errorMsg := tasksLoadedMsg{err: context.DeadlineExceeded}

	newModel2, _ := updated.Update(errorMsg)
	updated2 := newModel2.(*taskListModel)

	if updated2.pageOffset != 0 {
		t.Errorf("pageOffset should remain 0 after fetch error, got %d", updated2.pageOffset)
	}
	if updated2.pendingPageOffset != 0 {
		t.Errorf("pendingPageOffset should be cleared after error, got %d", updated2.pendingPageOffset)
	}
	if updated2.err == nil {
		t.Fatal("expected error to be set on model")
	}
	if len(updated2.tasks) != 1 {
		t.Errorf("expected 1 original task, got %d", len(updated2.tasks))
	}
}

func TestTaskListModel_PageNav_PrevClamped_ErrorNoAdvance(t *testing.T) {
	store := &mockTaskStore{
		ListTasksFunc: func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) {
			return []ports.Task{}, nil
		},
		CountTasksFunc: func(ctx context.Context, status string) (int, error) { return 100, nil },
	}

	m := newTaskListModel(context.Background(), store)
	m.ready = true
	m.tasks = []ports.Task{{ID: 1, Content: "at page 0", Status: "pending"}}
	m.totalCount = 100
	m.pageOffset = 0
	m.selected = 0
	m.pageSize = 50

	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	updated := newModel.(*taskListModel)

	if updated.pendingPageOffset != 0 {
		t.Errorf("pendingPageOffset should be 0 (clamped), got %d", updated.pendingPageOffset)
	}

	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	cmd()

	newModel2, _ := updated.Update(tasksLoadedMsg{err: context.DeadlineExceeded})
	updated2 := newModel2.(*taskListModel)

	if updated2.pageOffset != 0 {
		t.Errorf("pageOffset should remain 0, got %d", updated2.pageOffset)
	}
	if updated2.err == nil {
		t.Fatal("expected error to be set")
	}
}

func TestTaskListModel_CmdNextPage(t *testing.T) {
	tests := []struct {
		name              string
		pageOffset        int
		pageSize          int
		totalCount        int
		wantPendingOffset int
	}{
		{"advances when room", 0, 50, 100, 50},
		{"clamped at boundary", 50, 50, 100, 50},
		{"clamped past boundary", 60, 50, 100, 60},
		{"exact boundary", 50, 50, 100, 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &taskListModel{
				ctx:        context.Background(),
				pageOffset: tt.pageOffset,
				pageSize:   tt.pageSize,
				totalCount: tt.totalCount,
			}
			cmd := m.cmdNextPage()
			if cmd == nil {
				t.Fatal("expected non-nil cmd from cmdNextPage")
			}
			if m.pendingPageOffset != tt.wantPendingOffset {
				t.Errorf("pendingPageOffset = %d, want %d", m.pendingPageOffset, tt.wantPendingOffset)
			}
			if !m.pageNavPending {
				t.Error("expected pageNavPending to be true")
			}
			if m.pageOffset != tt.pageOffset {
				t.Errorf("pageOffset should not change until fetch succeeds, got %d want %d", m.pageOffset, tt.pageOffset)
			}
		})
	}
}

func TestTaskListModel_CmdPrevPage(t *testing.T) {
	tests := []struct {
		name              string
		pageOffset        int
		pageSize          int
		wantPendingOffset int
	}{
		{"decrements from 50", 50, 50, 0},
		{"clamped at zero", 0, 50, 0},
		{"partial decrement", 30, 50, 0},
		{"exact decrement", 100, 50, 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &taskListModel{
				ctx:        context.Background(),
				pageOffset: tt.pageOffset,
				pageSize:   tt.pageSize,
			}
			cmd := m.cmdPrevPage()
			if cmd == nil {
				t.Fatal("expected non-nil cmd from cmdPrevPage")
			}
			if m.pendingPageOffset != tt.wantPendingOffset {
				t.Errorf("pendingPageOffset = %d, want %d", m.pendingPageOffset, tt.wantPendingOffset)
			}
			if !m.pageNavPending {
				t.Error("expected pageNavPending to be true")
			}
			if m.pageOffset != tt.pageOffset {
				t.Errorf("pageOffset should not change until fetch succeeds, got %d want %d", m.pageOffset, tt.pageOffset)
			}
		})
	}
}

// ── Error recovery tests (G7) ──

func TestTaskListModel_ErrorRecovery_RetryOnAnyKey(t *testing.T) {
	store := &mockTaskStore{
		ListTasksFunc: func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) {
			return []ports.Task{{ID: 1, Content: "recovered", Status: "pending"}}, nil
		},
		CountTasksFunc: func(ctx context.Context, status string) (int, error) { return 1, nil },
	}

	m := &taskListModel{
		ctx:      context.Background(),
		provider: store,
		err:      errors.New("transient failure"),
		ready:    true,
	}

	// Press 'r' (any non-quit key)
	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	updated := newModel.(*taskListModel)

	if updated.err != nil {
		t.Errorf("expected error to be cleared on retry, got %v", updated.err)
	}
	if cmd == nil {
		t.Fatal("expected non-nil fetch cmd on retry")
	}

	msg := cmd()
	tasksMsg, ok := msg.(tasksLoadedMsg)
	if !ok {
		t.Fatalf("expected tasksLoadedMsg, got %T", msg)
	}
	if tasksMsg.err != nil {
		t.Fatalf("unexpected fetch error: %v", tasksMsg.err)
	}
	if len(tasksMsg.tasks) != 1 {
		t.Errorf("expected 1 task on retry, got %d", len(tasksMsg.tasks))
	}
}

func TestTaskListModel_ErrorRecovery_QuitKeys(t *testing.T) {
	tests := []struct {
		name string
		key  tea.KeyMsg
	}{
		{"q quits", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}},
		{"esc quits", tea.KeyMsg{Type: tea.KeyEsc}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &taskListModel{
				err:   errors.New("some error"),
				ready: true,
			}
			_, cmd := m.Update(tt.key)
			if cmd == nil {
				t.Fatal("expected quit command")
			}
		})
	}
}

func TestTaskListModel_ErrorRecovery_IgnoresNonKeyMsg(t *testing.T) {
	m := &taskListModel{
		err:   errors.New("some error"),
		ready: true,
	}

	_, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	if m.err == nil {
		t.Error("expected error to persist for non-key messages")
	}
	if cmd != nil {
		t.Error("expected nil cmd for non-key messages in error state")
	}
}

func TestTaskListModel_ErrorRecovery_ViewShowsRetryHint(t *testing.T) {
	m := &taskListModel{
		err:   errors.New("boom"),
		ready: true,
	}
	view := m.View()
	if !strings.Contains(view, "retry") {
		t.Errorf("expected 'retry' hint in error view, got %q", view)
	}
	if !strings.Contains(view, "Error: boom") {
		t.Errorf("expected error message in view, got %q", view)
	}
}

// ── handleViewportUpdate nil-guard tests (G2) ──

func TestTaskListModel_HandleViewportUpdate_NotReady(t *testing.T) {
	m := &taskListModel{ready: false}

	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	_ = newModel

	if cmd != nil {
		t.Error("expected nil cmd from handleViewportUpdate when not ready")
	}
}

func TestTaskListModel_HandleViewportUpdate_Ready(t *testing.T) {
	store := &mockTaskStore{
		ListTasksFunc:  func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) { return nil, nil },
		CountTasksFunc: func(ctx context.Context, status string) (int, error) { return 0, nil },
	}
	m := newTaskListModel(context.Background(), store)
	m.handleWindowSizeMsg(tea.WindowSizeMsg{Width: 80, Height: 40})

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	_ = cmd
}

// ── G6: Nil provider guard ──

func TestNewTaskListModel_NilProvider(t *testing.T) {
	m := newTaskListModel(context.Background(), nil)
	if m.err == nil {
		t.Fatal("expected error for nil provider")
	}
	if !strings.Contains(m.err.Error(), "not available") {
		t.Errorf("expected 'not available' in error, got: %v", m.err)
	}
	m.ready = true
	view := m.View()
	if !strings.Contains(view, "Error:") {
		t.Errorf("expected error in View, got %q", view)
	}
}

func TestTaskListModel_Init_NilProvider(t *testing.T) {
	m := newTaskListModel(context.Background(), nil)
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	msg := cmd()
	tasksMsg, ok := msg.(tasksLoadedMsg)
	if !ok {
		t.Fatalf("expected tasksLoadedMsg, got %T", msg)
	}
	if tasksMsg.err == nil {
		t.Fatal("expected error in tasksLoadedMsg")
	}
}

// ── G5: Context cancellation in fetchTasksCmd ──

func TestFetchTasksCmd_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := &mockTaskStore{
		ListTasksFunc: func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) {
			t.Error("ListTasks should not be called")
			return nil, nil
		},
		CountTasksFunc: func(ctx context.Context, status string) (int, error) {
			t.Error("CountTasks should not be called")
			return 0, nil
		},
	}
	cmd := fetchTasksCmd(ctx, store, "", 0, 50)
	msg := cmd()
	tasksMsg := msg.(tasksLoadedMsg)
	if tasksMsg.err == nil {
		t.Fatal("expected context cancellation error")
	}
}

// ── fetchTasksCmd error path tests (Issue #1024) ──

func TestFetchTasksCmd_ListTasksError(t *testing.T) {
	store := &mockTaskStore{
		ListTasksFunc: func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) {
			return nil, errors.New("db connection refused")
		},
		CountTasksFunc: func(ctx context.Context, status string) (int, error) {
			t.Error("CountTasks should not be called when ListTasks fails")
			return 0, nil
		},
	}

	cmd := fetchTasksCmd(context.Background(), store, "", 0, 50)
	msg := cmd()
	tasksMsg, ok := msg.(tasksLoadedMsg)
	if !ok {
		t.Fatalf("expected tasksLoadedMsg, got %T", msg)
	}
	if tasksMsg.err == nil {
		t.Fatal("expected error from ListTasks failure, got nil")
	}
	if tasksMsg.err.Error() != "db connection refused" {
		t.Errorf("expected error 'db connection refused', got %q", tasksMsg.err.Error())
	}
	if tasksMsg.tasks != nil {
		t.Errorf("expected nil tasks on error, got %v", tasksMsg.tasks)
	}
	if tasksMsg.totalCount != 0 {
		t.Errorf("expected totalCount 0 on error, got %d", tasksMsg.totalCount)
	}
}

func TestFetchTasksCmd_CountTasksError(t *testing.T) {
	store := &mockTaskStore{
		ListTasksFunc: func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) {
			return []ports.Task{
				{ID: 1, Content: "survived list", Status: "pending"},
			}, nil
		},
		CountTasksFunc: func(ctx context.Context, status string) (int, error) {
			return 0, errors.New("count query timed out")
		},
	}

	cmd := fetchTasksCmd(context.Background(), store, "pending", 0, 50)
	msg := cmd()
	tasksMsg, ok := msg.(tasksLoadedMsg)
	if !ok {
		t.Fatalf("expected tasksLoadedMsg, got %T", msg)
	}
	if tasksMsg.err == nil {
		t.Fatal("expected error from CountTasks failure, got nil")
	}
	if tasksMsg.err.Error() != "count query timed out" {
		t.Errorf("expected error 'count query timed out', got %q", tasksMsg.err.Error())
	}
	if tasksMsg.tasks != nil {
		t.Errorf("expected nil tasks when CountTasks fails (partial result discarded), got %v", tasksMsg.tasks)
	}
	if tasksMsg.totalCount != 0 {
		t.Errorf("expected totalCount 0 on error, got %d", tasksMsg.totalCount)
	}
}

// ── G3: Unknown message handling ──

func TestTaskListModel_Update_UnknownMessageNotReady(t *testing.T) {
	store := &mockTaskStore{
		ListTasksFunc:  func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) { return nil, nil },
		CountTasksFunc: func(ctx context.Context, status string) (int, error) { return 0, nil },
	}
	m := newTaskListModel(context.Background(), store)
	_, cmd := m.Update(tea.BatchMsg{})
	if cmd != nil {
		t.Error("expected nil cmd when not ready")
	}
}

func TestTaskListModel_Update_UnknownMessageReady(t *testing.T) {
	store := &mockTaskStore{
		ListTasksFunc:  func(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) { return nil, nil },
		CountTasksFunc: func(ctx context.Context, status string) (int, error) { return 0, nil },
	}
	m := newTaskListModel(context.Background(), store)
	m.ready = true
	m.viewport = viewport.New(80, 24)
	_, _ = m.Update(tea.BatchMsg{})
}
