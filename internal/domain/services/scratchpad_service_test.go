// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package services

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

type mockScratchRepo struct {
	content string
	err     error
}

func (m *mockScratchRepo) Get(ctx context.Context, key string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	if key == "content" {
		return m.content, nil
	}
	return "", nil
}

func (m *mockScratchRepo) Set(ctx context.Context, key, val string) error {
	if m.err != nil {
		return m.err
	}
	if key == "content" {
		m.content = val
	}
	return nil
}

func (m *mockScratchRepo) Delete(ctx context.Context, key string) error {
	if m.err != nil {
		return m.err
	}
	if key == "content" {
		m.content = ""
	}
	return nil
}

func (m *mockScratchRepo) GetAll(ctx context.Context) (map[string]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	return map[string]string{"content": m.content}, nil
}

func TestScratchpadService(t *testing.T) {
	ctx := context.Background()

	t.Run("Initialize_Success", func(t *testing.T) {
		repo := &mockScratchRepo{content: "initial data"}
		s := NewScratchpadService(repo)
		if err := s.Initialize(ctx); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.Read() != "initial data" {
			t.Errorf("expected 'initial data', got %s", s.Read())
		}
	})

	t.Run("Initialize_Error", func(t *testing.T) {
		repo := &mockScratchRepo{err: fmt.Errorf("read error")}
		s := NewScratchpadService(repo)
		if err := s.Initialize(ctx); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("Write and Read", func(t *testing.T) {
		repo := &mockScratchRepo{}
		s := NewScratchpadService(repo)
		if err := s.Write(ctx, "Hello"); err != nil {
			t.Fatal(err)
		}
		if s.Read() != "Hello" {
			t.Errorf("expected Hello, got %s", s.Read())
		}
	})

	t.Run("Write_Failure_DoesNotUpdateState", func(t *testing.T) {
		repo := &mockScratchRepo{content: "original"}
		s := NewScratchpadService(repo)
		_ = s.Initialize(ctx)

		repo.err = fmt.Errorf("disk full")
		err := s.Write(ctx, "new content")

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if s.Read() != "original" {
			t.Errorf("expected state to stay 'original', got %s", s.Read())
		}
	})

	t.Run("Append", func(t *testing.T) {
		repo := &mockScratchRepo{}
		s := NewScratchpadService(repo)
		if err := s.Append(ctx, "Hello"); err != nil {
			t.Fatal(err)
		}
		if err := s.Append(ctx, "World"); err != nil {
			t.Fatal(err)
		}
		if s.Read() != "Hello\nWorld" {
			t.Errorf("expected Hello\nWorld, got %s", s.Read())
		}
	})

	t.Run("Append_Failure_DoesNotUpdateState", func(t *testing.T) {
		repo := &mockScratchRepo{content: "original"}
		s := NewScratchpadService(repo)
		_ = s.Initialize(ctx)

		repo.err = fmt.Errorf("disk full")
		err := s.Append(ctx, "new content")

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if s.Read() != "original" {
			t.Errorf("expected state to stay 'original', got %s", s.Read())
		}
	})

	t.Run("Clear", func(t *testing.T) {
		repo := &mockScratchRepo{content: "something"}
		s := NewScratchpadService(repo)
		_ = s.Initialize(ctx)

		if err := s.Clear(ctx); err != nil {
			t.Fatal(err)
		}
		if s.Read() != "" {
			t.Error("expected empty scratchpad")
		}
	})

	t.Run("Clear_Failure_DoesNotUpdateState", func(t *testing.T) {
		repo := &mockScratchRepo{content: "original"}
		s := NewScratchpadService(repo)
		_ = s.Initialize(ctx)

		repo.err = fmt.Errorf("delete error")
		err := s.Clear(ctx)

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if s.Read() != "original" {
			t.Errorf("expected state to stay 'original', got %s", s.Read())
		}
	})
}

func TestScratchpadService_Concurrency(t *testing.T) {
	ctx := context.Background()
	repo := &mockScratchRepo{}
	s := NewScratchpadService(repo)

	var wg sync.WaitGroup
	iterations := 100

	wg.Add(3)

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = s.Write(ctx, fmt.Sprintf("val-%d", i))
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = s.Append(ctx, "suffix")
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = s.Read()
		}
	}()

	wg.Wait()
}
