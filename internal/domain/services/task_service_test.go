// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package services

import (
	"context"
	"testing"
)

type mockTaskRepo struct {
	tasks []Task
}

func (m *mockTaskRepo) LoadTasks(ctx context.Context) ([]Task, error) { return m.tasks, nil }
func (m *mockTaskRepo) SaveTasks(ctx context.Context, tasks []Task) error {
	m.tasks = tasks
	return nil
}

func TestTaskService(t *testing.T) {
	ctx := context.Background()
	repo := &mockTaskRepo{}
	s := NewTaskService(repo)

	t.Run("Add Task", func(t *testing.T) {
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
	})

	t.Run("Update Task", func(t *testing.T) {
		_, err := s.UpdateTask(ctx, 1, "Updated task", "completed")
		if err != nil {
			t.Fatal(err)
		}
		tasks := s.ListTasks("")
		if len(tasks) != 1 || tasks[0].Content != "Updated task" || tasks[0].Status != "completed" {
			t.Errorf("unexpected task: %+v", tasks[0])
		}
	})

	t.Run("Delete Task", func(t *testing.T) {
		if err := s.DeleteTask(ctx, 1); err != nil {
			t.Fatal(err)
		}
		if len(s.ListTasks("")) != 0 {
			t.Error("task not deleted")
		}
	})
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
