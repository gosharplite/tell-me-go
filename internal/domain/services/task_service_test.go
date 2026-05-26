// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package services

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

type mockTaskRepo struct {
	tasks    []ports.Task
	readErr  error
	writeErr error
}

func (m *mockTaskRepo) ReadAll(ctx context.Context) ([]ports.Task, error) { return m.tasks, m.readErr }
func (m *mockTaskRepo) Update(ctx context.Context, id int64, task ports.Task) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	for i, t := range m.tasks {
		if t.ID == id {
			m.tasks[i] = task
			return nil
		}
	}
	return nil
}

func (m *mockTaskRepo) Delete(ctx context.Context, id int64) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	var next []ports.Task
	for _, t := range m.tasks {
		if t.ID != id {
			next = append(next, t)
		}
	}
	m.tasks = next
	return nil
}

func (m *mockTaskRepo) DeleteAll(ctx context.Context) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	m.tasks = nil
	return nil
}

func (m *mockTaskRepo) Append(ctx context.Context, task ports.Task) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	m.tasks = append(m.tasks, task)
	return nil
}

func (m *mockTaskRepo) Query(ctx context.Context, filter ports.ListFilter, limit, offset int) ([]ports.Task, error) {
	if m.readErr != nil {
		return nil, m.readErr
	}
	var result []ports.Task
	for _, t := range m.tasks {
		if taskMatchesFilter(t, filter) {
			result = append(result, t)
		}
	}
	return applyTaskOffsetLimit(result, limit, offset), nil
}

// taskMatchesFilter returns true when task satisfies all non-zero filter conditions.
func taskMatchesFilter(task ports.Task, filter ports.ListFilter) bool {
	if filter.Status != "" && task.Status != filter.Status {
		return false
	}
	if filter.NotStatus != "" && task.Status == filter.NotStatus {
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

// applyTaskOffsetLimit applies offset/limit slicing to a task slice.
func applyTaskOffsetLimit(tasks []ports.Task, limit, offset int) []ports.Task {
	if offset > 0 {
		if offset >= len(tasks) {
			return []ports.Task{}
		}
		tasks = tasks[offset:]
	}
	if limit > 0 && limit < len(tasks) {
		tasks = tasks[:limit]
	}
	return tasks
}

func (m *mockTaskRepo) Count(ctx context.Context) (int, error) {
	if m.readErr != nil {
		return 0, m.readErr
	}
	return len(m.tasks), nil
}

func setupTaskService(t *testing.T) (ports.TaskStore, *mockTaskRepo) {
	t.Helper()
	repo := &mockTaskRepo{}
	s := NewTaskService(repo)
	return s, repo
}

func TestTaskService_Add(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, repo := setupTaskService(t)

	task, err := s.AddTask(ctx, "Test task")
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != 1 || task.Content != "Test task" {
		t.Errorf("unexpected task: %+v", task)
	}
	if len(repo.tasks) != 1 {
		t.Error("task not saved to repo")
	}
}

func TestTaskService_Update(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := setupTaskService(t)
	_, _ = s.AddTask(ctx, "Initial task")

	_, err := s.UpdateTask(ctx, 1, "Updated task", "completed")
	if err != nil {
		t.Fatal(err)
	}
	tasks := s.ListTasks("", 0, 0)
	if len(tasks) != 1 || tasks[0].Content != "Updated task" || tasks[0].Status != "completed" {
		t.Errorf("unexpected task: %+v", tasks[0])
	}
}

func TestTaskService_Delete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := setupTaskService(t)
	_, _ = s.AddTask(ctx, "To be deleted")

	if err := s.DeleteTask(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if len(s.ListTasks("", 0, 0)) != 0 {
		t.Error("task not deleted")
	}
}

func TestTaskService_Concurrency(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := &mockTaskRepo{}
	s := NewTaskService(repo)

	const workers = 100
	done := make(chan bool)
	for i := 0; i < workers; i++ {
		go func(val int) {
			_, _ = s.AddTask(ctx, "Task")
			_ = s.ListTasks("", 0, 0)
			done <- true
		}(i)
	}

	for i := 0; i < workers; i++ {
		<-done
	}

	if len(s.ListTasks("", 0, 0)) != workers {
		t.Errorf("expected %d tasks, got %d", workers, len(s.ListTasks("", 0, 0)))
	}
}

func TestTaskService_Initialize(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		repo := &mockTaskRepo{
			tasks: []ports.Task{
				{ID: 1, Content: "Task 1", Status: "pending"},
				{ID: 10, Content: "Task 10", Status: "pending"},
			},
		}
		s := NewTaskService(repo)

		err := s.Initialize(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		tasks := s.ListTasks("", 0, 0)
		if len(tasks) != 2 {
			t.Errorf("expected 2 tasks, got %d", len(tasks))
		}

		// Verify nextID is max(ID) + 1 = 11
		newTask, err := s.AddTask(ctx, "New Task")
		if err != nil {
			t.Fatalf("failed to add task after init: %v", err)
		}
		if newTask.ID != 11 {
			t.Errorf("expected new task ID to be 11, got %v", newTask.ID)
		}
	})

	t.Run("Error", func(t *testing.T) {
		t.Parallel()
		repo := &mockTaskRepo{
			readErr: errors.New("read error"),
		}
		s := NewTaskService(repo)

		err := s.Initialize(ctx)
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

func TestTaskService_ClearTasks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		s, repo := setupTaskService(t)
		_, _ = s.AddTask(ctx, "Task 1")
		_, _ = s.AddTask(ctx, "Task 2")

		err := s.ClearTasks(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(s.ListTasks("", 0, 0)) != 0 {
			t.Error("tasks not cleared from service")
		}
		if len(repo.tasks) != 0 {
			t.Error("tasks not cleared from repo")
		}
	})

	t.Run("Error", func(t *testing.T) {
		t.Parallel()
		repo := &mockTaskRepo{}
		s := NewTaskService(repo)
		_, err := s.AddTask(ctx, "Task")
		if err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		repo.writeErr = errors.New("write error")
		err = s.ClearTasks(ctx)
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

func TestTaskService_ErrorPaths(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("AddTask Empty Content", func(t *testing.T) {
		t.Parallel()
		s, _ := setupTaskService(t)
		_, err := s.AddTask(ctx, "")
		if err == nil {
			t.Error("expected error for empty content")
		}
	})

	t.Run("AddTask Write Error", func(t *testing.T) {
		t.Parallel()
		repo := &mockTaskRepo{writeErr: errors.New("write fail")}
		s := NewTaskService(repo)
		_, err := s.AddTask(ctx, "Test")
		if err == nil {
			t.Error("expected write error")
		}
	})

	t.Run("UpdateTask Not Found", func(t *testing.T) {
		t.Parallel()
		s, _ := setupTaskService(t)
		_, err := s.UpdateTask(ctx, 999, "content", "status")
		if err == nil {
			t.Error("expected not found error")
		}
	})

	t.Run("UpdateTask Write Error", func(t *testing.T) {
		t.Parallel()
		s, repo := setupTaskService(t)
		_, _ = s.AddTask(ctx, "Task")
		repo.writeErr = errors.New("write fail")
		_, err := s.UpdateTask(ctx, 1, "Updated", "completed")
		if err == nil {
			t.Error("expected write error")
		}
	})

	t.Run("DeleteTask Not Found", func(t *testing.T) {
		t.Parallel()
		s, _ := setupTaskService(t)
		err := s.DeleteTask(ctx, 999)
		if err == nil {
			t.Error("expected not found error")
		}
	})

	t.Run("DeleteTask Write Error", func(t *testing.T) {
		t.Parallel()
		s, repo := setupTaskService(t)
		_, _ = s.AddTask(ctx, "Task")
		repo.writeErr = errors.New("write fail")
		err := s.DeleteTask(ctx, 1)
		if err == nil {
			t.Error("expected write error")
		}
	})
}

func TestTaskService_ListTasks_Filter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := setupTaskService(t)
	_, _ = s.AddTask(ctx, "Pending 1")
	_, _ = s.AddTask(ctx, "Pending 2")
	t3, _ := s.AddTask(ctx, "Completed")
	_, _ = s.UpdateTask(ctx, t3.ID, "", "completed")

	pending := s.ListTasks("pending", 0, 0)
	if len(pending) != 2 {
		t.Errorf("expected 2 pending tasks, got %d", len(pending))
	}

	completed := s.ListTasks("completed", 0, 0)
	if len(completed) != 1 {
		t.Errorf("expected 1 completed task, got %d", len(completed))
	}
}

func TestTaskService_UpdateTask_Partial(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := setupTaskService(t)
	t1, _ := s.AddTask(ctx, "Task 1")

	// Update only content
	t1, _ = s.UpdateTask(ctx, t1.ID, "New Content", "")
	if t1.Content != "New Content" || t1.Status != "pending" {
		t.Errorf("unexpected update: %+v", t1)
	}

	// Update only status
	t1, _ = s.UpdateTask(ctx, t1.ID, "", "done")
	if t1.Content != "New Content" || t1.Status != "done" {
		t.Errorf("unexpected update: %+v", t1)
	}
}

func TestTaskService_DeleteTask_Multiple(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, repo := setupTaskService(t)
	_, _ = s.AddTask(ctx, "Task 1")
	t2, _ := s.AddTask(ctx, "Task 2")
	_, _ = s.AddTask(ctx, "Task 3")

	err := s.DeleteTask(ctx, t2.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tasks := s.ListTasks("", 0, 0)
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(tasks))
	}
	if len(repo.tasks) != 2 {
		t.Error("task not deleted from repo")
	}
}

func TestTaskService_Initialize_OnlyActive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	repo := &mockTaskRepo{
		tasks: []ports.Task{
			{ID: 1, Content: "Pending 1", Status: "pending"},
			{ID: 2, Content: "In Progress", Status: "in_progress"},
			{ID: 3, Content: "Completed 1", Status: "completed"},
			{ID: 4, Content: "Completed 2", Status: "completed"},
		},
	}
	s := NewTaskService(repo)

	err := s.Initialize(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only pending and in_progress should be loaded
	tasks := s.ListTasks("", 0, 0)
	if len(tasks) != 2 {
		t.Errorf("expected 2 active tasks, got %d: %+v", len(tasks), tasks)
	}
	for _, task := range tasks {
		if task.Status == "completed" {
			t.Errorf("completed task %d should not have been loaded", int(task.ID))
		}
	}

	// nextID should be max of active IDs + 1 = 3 (not 5 from completed tasks)
	if s.nextID != 3 {
		t.Errorf("expected nextID 3, got %v", s.nextID)
	}
}

func TestTaskService_CountTasks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name     string
		setup    func(s ports.TaskStore)
		status   string
		expected int
	}{
		{
			name:     "empty store",
			setup:    func(s ports.TaskStore) {},
			status:   "",
			expected: 0,
		},
		{
			name: "all pending - count all",
			setup: func(s ports.TaskStore) {
				_, _ = s.AddTask(ctx, "Task 1")
				_, _ = s.AddTask(ctx, "Task 2")
				_, _ = s.AddTask(ctx, "Task 3")
			},
			status:   "",
			expected: 3,
		},
		{
			name: "all pending - filter pending",
			setup: func(s ports.TaskStore) {
				_, _ = s.AddTask(ctx, "Task 1")
				_, _ = s.AddTask(ctx, "Task 2")
				_, _ = s.AddTask(ctx, "Task 3")
			},
			status:   "pending",
			expected: 3,
		},
		{
			name: "all pending - filter completed",
			setup: func(s ports.TaskStore) {
				_, _ = s.AddTask(ctx, "Task 1")
				_, _ = s.AddTask(ctx, "Task 2")
				_, _ = s.AddTask(ctx, "Task 3")
			},
			status:   "completed",
			expected: 0,
		},
		{
			name: "mixed - count all",
			setup: func(s ports.TaskStore) {
				_, _ = s.AddTask(ctx, "Pending 1")
				_, _ = s.AddTask(ctx, "Pending 2")
				t, _ := s.AddTask(ctx, "Completed")
				_, _ = s.UpdateTask(ctx, t.ID, "", "completed")
			},
			status:   "",
			expected: 3,
		},
		{
			name: "mixed - filter pending",
			setup: func(s ports.TaskStore) {
				_, _ = s.AddTask(ctx, "Pending 1")
				_, _ = s.AddTask(ctx, "Pending 2")
				t, _ := s.AddTask(ctx, "Completed")
				_, _ = s.UpdateTask(ctx, t.ID, "", "completed")
			},
			status:   "pending",
			expected: 2,
		},
		{
			name: "mixed - filter completed",
			setup: func(s ports.TaskStore) {
				_, _ = s.AddTask(ctx, "Pending 1")
				_, _ = s.AddTask(ctx, "Pending 2")
				t, _ := s.AddTask(ctx, "Completed")
				_, _ = s.UpdateTask(ctx, t.ID, "", "completed")
			},
			status:   "completed",
			expected: 1,
		},
		{
			name: "after clear",
			setup: func(s ports.TaskStore) {
				_, _ = s.AddTask(ctx, "Task 1")
				_, _ = s.AddTask(ctx, "Task 2")
				_, _ = s.AddTask(ctx, "Task 3")
				_ = s.ClearTasks(ctx)
			},
			status:   "",
			expected: 0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s, _ := setupTaskService(t)
			tt.setup(s)

			got := s.CountTasks(tt.status)
			if got != tt.expected {
				t.Errorf("CountTasks(%q) = %d; want %d", tt.status, got, tt.expected)
			}
		})
	}
}

// seedTaskServiceWithN adds n tasks ("Task 1" through "Task N") to the store.
func seedTaskServiceWithN(t *testing.T, s ports.TaskStore, n int) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < n; i++ {
		_, err := s.AddTask(ctx, fmt.Sprintf("Task %d", i+1))
		if err != nil {
			t.Fatalf("failed to add task: %v", err)
		}
	}
}

func TestTaskService_ListTasks_LimitOffset(t *testing.T) {
	t.Parallel()

	t.Run("limit3_offset0_returns_first_3", func(t *testing.T) {
		t.Parallel()
		s, _ := setupTaskService(t)
		seedTaskServiceWithN(t, s, 5)

		tasks := s.ListTasks("", 3, 0)
		if len(tasks) != 3 {
			t.Errorf("expected 3 tasks, got %d", len(tasks))
		}
		if tasks[0].ID != 1 || tasks[2].ID != 3 {
			t.Errorf("expected IDs 1,2,3, got %v", tasks)
		}
	})

	t.Run("limit2_offset3_returns_tasks_4_5", func(t *testing.T) {
		t.Parallel()
		s, _ := setupTaskService(t)
		seedTaskServiceWithN(t, s, 5)

		tasks := s.ListTasks("", 2, 3)
		if len(tasks) != 2 {
			t.Errorf("expected 2 tasks, got %d", len(tasks))
		}
		if tasks[0].ID != 4 || tasks[1].ID != 5 {
			t.Errorf("expected IDs 4,5, got %v", tasks)
		}
	})

	t.Run("offset_beyond_total_returns_empty", func(t *testing.T) {
		t.Parallel()
		s, _ := setupTaskService(t)
		seedTaskServiceWithN(t, s, 5)

		tasks := s.ListTasks("", 10, 100)
		if len(tasks) != 0 {
			t.Errorf("expected 0 tasks, got %d", len(tasks))
		}
	})

	t.Run("limit0_returns_all", func(t *testing.T) {
		t.Parallel()
		s, _ := setupTaskService(t)
		seedTaskServiceWithN(t, s, 5)

		tasks := s.ListTasks("", 0, 0)
		if len(tasks) != 5 {
			t.Errorf("expected 5 tasks, got %d", len(tasks))
		}
	})
}
