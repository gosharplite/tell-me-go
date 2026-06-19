// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package services

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

type mockTaskRepo struct {
	mu       sync.Mutex
	tasks    []ports.Task
	readErr  error
	writeErr error
}

func (m *mockTaskRepo) ReadAll(ctx context.Context) ([]ports.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tasks, m.readErr
}
func (m *mockTaskRepo) Update(ctx context.Context, id int64, task ports.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
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
	m.mu.Lock()
	defer m.mu.Unlock()
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.writeErr != nil {
		return m.writeErr
	}
	m.tasks = nil
	return nil
}

func (m *mockTaskRepo) Append(ctx context.Context, task ports.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.writeErr != nil {
		return m.writeErr
	}
	m.tasks = append(m.tasks, task)
	return nil
}

func (m *mockTaskRepo) Query(ctx context.Context, filter ports.ListFilter, limit, offset int) ([]ports.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
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
	m.mu.Lock()
	defer m.mu.Unlock()
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
	if task.ID == 0 || task.Content != "Test task" {
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
	task, _ := s.AddTask(ctx, "Initial task")

	_, err := s.UpdateTask(ctx, task.ID, "Updated task", "completed")
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := s.ListTasks(ctx, "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Content != "Updated task" || tasks[0].Status != "completed" {
		t.Errorf("unexpected task: %+v", tasks[0])
	}
}

func TestTaskService_Delete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := setupTaskService(t)
	task, _ := s.AddTask(ctx, "To be deleted")

	if err := s.DeleteTask(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	tasks, err := s.ListTasks(ctx, "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
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
			_, _ = s.ListTasks(context.Background(), "", 0, 0)
			done <- true
		}(i)
	}

	for i := 0; i < workers; i++ {
		<-done
	}

	tasks, err := s.ListTasks(ctx, "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != workers {
		t.Errorf("expected %d tasks, got %d", workers, len(tasks))
	}
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

		tasks, err := s.ListTasks(ctx, "", 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 0 {
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
		task, _ := s.AddTask(ctx, "Task")
		repo.writeErr = errors.New("write fail")
		_, err := s.UpdateTask(ctx, task.ID, "Updated", "completed")
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
		task, _ := s.AddTask(ctx, "Task")
		repo.writeErr = errors.New("write fail")
		err := s.DeleteTask(ctx, task.ID)
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

	pending, err := s.ListTasks(ctx, "pending", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 {
		t.Errorf("expected 2 pending tasks, got %d", len(pending))
	}

	completed, err := s.ListTasks(ctx, "completed", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
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

	tasks, err := s.ListTasks(ctx, "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(tasks))
	}
	if len(repo.tasks) != 2 {
		t.Error("task not deleted from repo")
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

			got, err := s.CountTasks(ctx, tt.status)
			if err != nil {
				t.Fatal(err)
			}
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
	ctx := context.Background()

	t.Run("limit3_offset0_returns_first_3", func(t *testing.T) {
		t.Parallel()
		s, _ := setupTaskService(t)
		seedTaskServiceWithN(t, s, 5)

		tasks, err := s.ListTasks(ctx, "", 3, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 3 {
			t.Errorf("expected 3 tasks, got %d", len(tasks))
		}
		if tasks[0].Content != "Task 1" || tasks[2].Content != "Task 3" {
			t.Errorf("expected Task 1, Task 2, Task 3, got %v", tasks)
		}
	})

	t.Run("limit2_offset3_returns_tasks_4_5", func(t *testing.T) {
		t.Parallel()
		s, _ := setupTaskService(t)
		seedTaskServiceWithN(t, s, 5)

		tasks, err := s.ListTasks(ctx, "", 2, 3)
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 2 {
			t.Errorf("expected 2 tasks, got %d", len(tasks))
		}
		if tasks[0].Content != "Task 4" || tasks[1].Content != "Task 5" {
			t.Errorf("expected Task 4, Task 5, got %v", tasks)
		}
	})

	t.Run("offset_beyond_total_returns_empty", func(t *testing.T) {
		t.Parallel()
		s, _ := setupTaskService(t)
		seedTaskServiceWithN(t, s, 5)

		tasks, err := s.ListTasks(ctx, "", 10, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 0 {
			t.Errorf("expected 0 tasks, got %d", len(tasks))
		}
	})

	t.Run("limit0_returns_all", func(t *testing.T) {
		t.Parallel()
		s, _ := setupTaskService(t)
		seedTaskServiceWithN(t, s, 5)

		tasks, err := s.ListTasks(ctx, "", 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 5 {
			t.Errorf("expected 5 tasks, got %d", len(tasks))
		}
	})
}
