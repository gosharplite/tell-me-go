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
	data map[string]string
	err  error
}

func (m *mockScratchRepo) Get(ctx context.Context, key string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.data[key], nil
}

func (m *mockScratchRepo) Set(ctx context.Context, key, val string) error {
	if m.err != nil {
		return m.err
	}
	if m.data == nil {
		m.data = make(map[string]string)
	}
	m.data[key] = val
	return nil
}

func (m *mockScratchRepo) Delete(ctx context.Context, key string) error {
	if m.err != nil {
		return m.err
	}
	delete(m.data, key)
	return nil
}

func (m *mockScratchRepo) GetAll(ctx context.Context) (map[string]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.data, nil
}

func TestScratchpadService_Initialize(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		repo := &mockScratchRepo{data: map[string]string{"content": "hello"}}
		s := NewScratchpadService(repo)
		err := s.Initialize(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if s.Read() != "hello" {
			t.Errorf("expected hello, got %s", s.Read())
		}
	})

	t.Run("Error", func(t *testing.T) {
		repo := &mockScratchRepo{err: fmt.Errorf("read error")}
		s := NewScratchpadService(repo)
		err := s.Initialize(ctx)
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

func TestScratchpadService_ReadWrite(t *testing.T) {
	ctx := context.Background()
	repo := &mockScratchRepo{data: make(map[string]string)}
	s := NewScratchpadService(repo)

	err := s.Write(ctx, "new content")
	if err != nil {
		t.Fatal(err)
	}
	if s.Read() != "new content" {
		t.Errorf("expected new content, got %s", s.Read())
	}
	if repo.data["content"] != "new content" {
		t.Errorf("repo not updated: %v", repo.data)
	}

	// Error path
	repo.err = fmt.Errorf("write error")
	err = s.Write(ctx, "fail")
	if err == nil {
		t.Error("expected write error")
	}
	// Verify rollback (internal state shouldn't change)
	if s.Read() != "new content" {
		t.Error("internal state changed on failure")
	}
}

func TestScratchpadService_Append(t *testing.T) {
	ctx := context.Background()
	repo := &mockScratchRepo{data: make(map[string]string)}
	s := NewScratchpadService(repo)

	_ = s.Write(ctx, "line 1")
	err := s.Append(ctx, "line 2")
	if err != nil {
		t.Fatal(err)
	}

	expected := "line 1\nline 2"
	if s.Read() != expected {
		t.Errorf("expected %q, got %q", expected, s.Read())
	}

	// Append to empty
	_ = s.Clear(ctx)
	_ = s.Append(ctx, "start")
	if s.Read() != "start" {
		t.Errorf("expected start, got %s", s.Read())
	}

	// Error path
	repo.err = fmt.Errorf("append error")
	err = s.Append(ctx, "more")
	if err == nil {
		t.Error("expected error")
	}
}

func TestScratchpadService_Clear(t *testing.T) {
	ctx := context.Background()
	repo := &mockScratchRepo{data: map[string]string{"content": "data"}}
	s := NewScratchpadService(repo)
	_ = s.Initialize(ctx)

	err := s.Clear(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.Read() != "" {
		t.Error("not cleared")
	}

	// Error path
	_ = s.Write(ctx, "data")
	repo.err = fmt.Errorf("delete error")
	err = s.Clear(ctx)
	if err == nil {
		t.Error("expected error")
	}
}

func TestScratchpadService_Concurrency(t *testing.T) {
	ctx := context.Background()
	repo := &mockScratchRepo{data: make(map[string]string)}
	s := NewScratchpadService(repo)

	const iterations = 100
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = s.Write(ctx, fmt.Sprintf("val %d", i))
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
