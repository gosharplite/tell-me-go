// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package services

import (
	"context"
	"testing"
)

type mockConfigRepo struct {
	config map[string]string
}

func (m *mockConfigRepo) Get(ctx context.Context, key string) (string, error) {
	return m.config[key], nil
}

func (m *mockConfigRepo) Set(ctx context.Context, key, val string) error {
	m.config[key] = val
	return nil
}

func (m *mockConfigRepo) Delete(ctx context.Context, key string) error {
	delete(m.config, key)
	return nil
}

func (m *mockConfigRepo) GetAll(ctx context.Context) (map[string]string, error) {
	return m.config, nil
}

func TestConfigService(t *testing.T) {
	ctx := context.Background()
	repo := &mockConfigRepo{config: make(map[string]string)}
	s := NewConfigService(repo)

	t.Run("Set and Get", func(t *testing.T) {
		if err := s.Set(ctx, "k1", "v1"); err != nil {
			t.Fatal(err)
		}
		val, err := s.Get("k1")
		if err != nil {
			t.Fatal(err)
		}
		if val != "v1" {
			t.Errorf("expected v1, got %s", val)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		if err := s.Delete(ctx, "k1"); err != nil {
			t.Fatal(err)
		}
		_, err := s.Get("k1")
		if err == nil {
			t.Error("expected error for deleted key")
		}
	})

	t.Run("GetAll", func(t *testing.T) {
		_ = s.Set(ctx, "k2", "v2")
		_ = s.Set(ctx, "k3", "v3")
		all := s.GetAll()
		if len(all) != 2 {
			t.Errorf("expected 2 items, got %d", len(all))
		}
	})
}
