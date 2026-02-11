// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package services

import (
	"context"
	"testing"
)

type mockScratchRepo struct {
	content string
}

func (m *mockScratchRepo) Get(ctx context.Context, key string) (string, error) {
	if key == "content" {
		return m.content, nil
	}
	return "", nil
}

func (m *mockScratchRepo) Set(ctx context.Context, key, val string) error {
	if key == "content" {
		m.content = val
	}
	return nil
}

func (m *mockScratchRepo) Delete(ctx context.Context, key string) error {
	if key == "content" {
		m.content = ""
	}
	return nil
}

func (m *mockScratchRepo) GetAll(ctx context.Context) (map[string]string, error) {
	return map[string]string{"content": m.content}, nil
}

func TestScratchpadService(t *testing.T) {
	ctx := context.Background()
	repo := &mockScratchRepo{}
	s := NewScratchpadService(repo)

	t.Run("Write and Read", func(t *testing.T) {
		if err := s.Write(ctx, "Hello"); err != nil {
			t.Fatal(err)
		}
		if s.Read() != "Hello" {
			t.Errorf("expected Hello, got %s", s.Read())
		}
	})

	t.Run("Append", func(t *testing.T) {
		if err := s.Append(ctx, "World"); err != nil {
			t.Fatal(err)
		}
		if s.Read() != "Hello\nWorld" {
			t.Errorf("expected Hello\nWorld, got %s", s.Read())
		}
	})

	t.Run("Clear", func(t *testing.T) {
		if err := s.Clear(ctx); err != nil {
			t.Fatal(err)
		}
		if s.Read() != "" {
			t.Error("expected empty scratchpad")
		}
	})
}
