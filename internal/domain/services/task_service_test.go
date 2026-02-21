// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package services

import (
	"context"
	"errors"
	"testing"
)

type mockTaskRepo struct {
	tasks    []Task
	readErr  error
	writeErr error
}

func (m *mockTaskRepo) ReadAll(ctx context.Context) ([]Task, error) { return m.tasks, m.readErr }
func (m *mockTaskRepo) Update(ctx context.Context, id float64, task Task) error {
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

func (m *mockTaskRepo) Delete(ctx context.Context, id float64) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	var next []Task
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

func (m *mockTaskRepo) Append(ctx context.Context, task Task) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	m.tasks = append(m.tasks, task)
	return nil
}

func setupTaskService(t *testing.T) (*TaskService, *mockTaskRepo) {
	t.Helper()
	repo := &mockTaskRepo{}
	s := NewTaskService(repo)
	return s, repo
}

func TestTaskService_Add(t *testing.T) {
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
	ctx := context.Background()
	s, _ := setupTaskService(t)
	_, _ = s.AddTask(ctx, "Initial task")

	_, err := s.UpdateTask(ctx, 1, "Updated task", "completed")
	if err != nil {
		t.Fatal(err)
	}
	tasks := s.ListTasks("")
	if len(tasks) != 1 || tasks[0].Content != "Updated task" || tasks[0].Status != "completed" {
		t.Errorf("unexpected task: %+v", tasks[0])
	}
}

func TestTaskService_Delete(t *testing.T) {
	ctx := context.Background()
	s, _ := setupTaskService(t)
	_, _ = s.AddTask(ctx, "To be deleted")

	if err := s.DeleteTask(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if len(s.ListTasks("")) != 0 {
		t.Error("task not deleted")
	}
}

func TestTaskService_Concurrency(t *testing.T) {
	ctx := context.Background()
	repo := &mockTaskRepo{}
	s := NewTaskService(repo)

	const workers = 100
	done := make(chan bool)
	for i := 0; i < workers; i++ {
		go func(val int) {
			_, _ = s.AddTask(ctx, "Task")
			_ = s.ListTasks("")
			done <- true
		}(i)
	}

	for i := 0; i < workers; i++ {
		<-done
	}

	if len(s.ListTasks("")) != workers {
		t.Errorf("expected %d tasks, got %d", workers, len(s.ListTasks("")))
	}
}

func TestTaskService_Initialize(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		repo := &mockTaskRepo{
			tasks: []Task{
				{ID: 1, Content: "Task 1"},
				{ID: 10, Content: "Task 10"},
			},
		}
		s := NewTaskService(repo)

		err := s.Initialize(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		tasks := s.ListTasks("")
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
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		s, repo := setupTaskService(t)
		_, _ = s.AddTask(ctx, "Task 1")
		_, _ = s.AddTask(ctx, "Task 2")

		err := s.ClearTasks(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(s.ListTasks("")) != 0 {
			t.Error("tasks not cleared from service")
		}
		if len(repo.tasks) != 0 {
			t.Error("tasks not cleared from repo")
		}
	})

	t.Run("Error", func(t *testing.T) {
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
	ctx := context.Background()

	t.Run("AddTask Empty Content", func(t *testing.T) {
		s, _ := setupTaskService(t)
		_, err := s.AddTask(ctx, "")
		if err == nil {
			t.Error("expected error for empty content")
		}
	})

	t.Run("AddTask Write Error", func(t *testing.T) {
		repo := &mockTaskRepo{writeErr: errors.New("write fail")}
		s := NewTaskService(repo)
		_, err := s.AddTask(ctx, "Test")
		if err == nil {
			t.Error("expected write error")
		}
	})

	t.Run("UpdateTask Not Found", func(t *testing.T) {
		s, _ := setupTaskService(t)
		_, err := s.UpdateTask(ctx, 999, "content", "status")
		if err == nil {
			t.Error("expected not found error")
		}
	})

	t.Run("UpdateTask Write Error", func(t *testing.T) {
		s, repo := setupTaskService(t)
		_, _ = s.AddTask(ctx, "Task")
		repo.writeErr = errors.New("write fail")
		_, err := s.UpdateTask(ctx, 1, "Updated", "completed")
		if err == nil {
			t.Error("expected write error")
		}
	})

	t.Run("DeleteTask Not Found", func(t *testing.T) {
		s, _ := setupTaskService(t)
		err := s.DeleteTask(ctx, 999)
		if err == nil {
			t.Error("expected not found error")
		}
	})

	t.Run("DeleteTask Write Error", func(t *testing.T) {
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
	ctx := context.Background()
	s, _ := setupTaskService(t)
	_, _ = s.AddTask(ctx, "Pending 1")
	_, _ = s.AddTask(ctx, "Pending 2")
	t3, _ := s.AddTask(ctx, "Completed")
	_, _ = s.UpdateTask(ctx, t3.ID, "", "completed")

	pending := s.ListTasks("pending")
	if len(pending) != 2 {
		t.Errorf("expected 2 pending tasks, got %d", len(pending))
	}

	completed := s.ListTasks("completed")
	if len(completed) != 1 {
		t.Errorf("expected 1 completed task, got %d", len(completed))
	}
}

func TestTaskService_UpdateTask_Partial(t *testing.T) {
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
	ctx := context.Background()
	s, repo := setupTaskService(t)
	_, _ = s.AddTask(ctx, "Task 1")
	t2, _ := s.AddTask(ctx, "Task 2")
	_, _ = s.AddTask(ctx, "Task 3")

	err := s.DeleteTask(ctx, t2.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tasks := s.ListTasks("")
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(tasks))
	}
	if len(repo.tasks) != 2 {
		t.Error("task not deleted from repo")
	}
}
