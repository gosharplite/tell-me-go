// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package services

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

type mockConfigRepo struct {
	config map[string]string
	err    error
}

func (m *mockConfigRepo) Get(ctx context.Context, key string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.config[key], nil
}

func (m *mockConfigRepo) Set(ctx context.Context, key, val string) error {
	if m.err != nil {
		return m.err
	}
	if m.config == nil {
		m.config = make(map[string]string)
	}
	m.config[key] = val
	return nil
}

func (m *mockConfigRepo) Delete(ctx context.Context, key string) error {
	if m.err != nil {
		return m.err
	}
	delete(m.config, key)
	return nil
}

func (m *mockConfigRepo) GetAll(ctx context.Context) (map[string]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	// Return a copy to simulate store behavior and avoid side effects
	res := make(map[string]string, len(m.config))
	for k, v := range m.config {
		res[k] = v
	}
	return res, nil
}

func TestConfigService_Initialize(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name         string
		repoData     map[string]string
		repoErr      error
		expectedErr  bool
		expectedData map[string]string
	}{
		{
			name: "Success",
			repoData: map[string]string{
				"key1": "val1",
				"key2": "val2",
			},
			repoErr:      nil,
			expectedErr:  false,
			expectedData: map[string]string{"key1": "val1", "key2": "val2"},
		},
		{
			name:         "Store Error",
			repoData:     nil,
			repoErr:      fmt.Errorf("store error"),
			expectedErr:  true,
			expectedData: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			repo := &mockConfigRepo{config: tt.repoData, err: tt.repoErr}
			s := NewConfigService(repo)

			err := s.Initialize(ctx)
			if (err != nil) != tt.expectedErr {
				t.Errorf("Initialize() error = %v, expectedErr %v", err, tt.expectedErr)
			}

			if !tt.expectedErr {
				if !reflect.DeepEqual(s.GetAll(), tt.expectedData) {
					t.Errorf("Initialize() loaded data = %v, expected %v", s.GetAll(), tt.expectedData)
				}
			}
		})
	}
}

func TestConfigService_Set(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("Validation_EmptyKey", func(t *testing.T) {
		t.Parallel()
		repo := &mockConfigRepo{config: make(map[string]string)}
		s := NewConfigService(repo)
		err := s.Set(ctx, "", "val")
		if err == nil || err.Error() != "key is required for set" {
			t.Errorf("expected 'key is required for set' error, got %v", err)
		}
	})

	t.Run("Success_NewKey", func(t *testing.T) {
		t.Parallel()
		repo := &mockConfigRepo{config: make(map[string]string)}
		s := NewConfigService(repo)
		err := s.Set(ctx, "k1", "v1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		val, _ := s.Get("k1")
		if val != "v1" {
			t.Errorf("expected v1, got %s", val)
		}
	})

	t.Run("RollbackOnFailure_NewKey", func(t *testing.T) {
		t.Parallel()
		repo := &mockConfigRepo{config: make(map[string]string)}
		s := NewConfigService(repo)
		_ = s.Initialize(ctx)

		repo.err = fmt.Errorf("persistent store failure")
		err := s.Set(ctx, "newkey", "newval")
		if err == nil {
			t.Fatal("expected error but got nil")
		}

		_, err = s.Get("newkey")
		if err == nil {
			t.Error("expected key to be removed from local cache on failure")
		}
	})

	t.Run("RollbackOnFailure_ExistingKey", func(t *testing.T) {
		t.Parallel()
		repo := &mockConfigRepo{config: map[string]string{"existing": "old"}}
		s := NewConfigService(repo)
		_ = s.Initialize(ctx)

		repo.err = fmt.Errorf("persistent store failure")
		err := s.Set(ctx, "existing", "new")
		if err == nil {
			t.Fatal("expected error but got nil")
		}

		val, _ := s.Get("existing")
		if val != "old" {
			t.Errorf("expected rollback to 'old', got %s", val)
		}
	})
}

func TestConfigService_Get(t *testing.T) {
	t.Parallel()
	repo := &mockConfigRepo{config: map[string]string{"k1": "v1"}}
	s := NewConfigService(repo)
	_ = s.Initialize(context.Background())

	tests := []struct {
		name        string
		key         string
		expectedVal string
		expectedErr bool
	}{
		{"Success", "k1", "v1", false},
		{"NotFound", "k2", "", true},
		{"EmptyKey", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			val, err := s.Get(tt.key)
			if (err != nil) != tt.expectedErr {
				t.Errorf("Get() error = %v, expectedErr %v", err, tt.expectedErr)
			}
			if val != tt.expectedVal {
				t.Errorf("Get() = %v, expected %v", val, tt.expectedVal)
			}
		})
	}
}

func TestConfigService_Delete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("Validation_EmptyKey", func(t *testing.T) {
		t.Parallel()
		repo := &mockConfigRepo{}
		s := NewConfigService(repo)
		err := s.Delete(ctx, "")
		if err == nil || err.Error() != "key is required for delete" {
			t.Errorf("expected 'key is required for delete' error, got %v", err)
		}
	})

	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		repo := &mockConfigRepo{config: map[string]string{"k1": "v1"}}
		s := NewConfigService(repo)
		_ = s.Initialize(ctx)

		err := s.Delete(ctx, "k1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		_, err = s.Get("k1")
		if err == nil {
			t.Error("expected key to be deleted")
		}
	})

	t.Run("Success_NonExistent", func(t *testing.T) {
		t.Parallel()
		repo := &mockConfigRepo{config: make(map[string]string)}
		s := NewConfigService(repo)
		err := s.Delete(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("RollbackOnFailure", func(t *testing.T) {
		t.Parallel()
		repo := &mockConfigRepo{config: map[string]string{"k1": "v1"}}
		s := NewConfigService(repo)
		_ = s.Initialize(ctx)

		repo.err = fmt.Errorf("delete failure")
		err := s.Delete(ctx, "k1")
		if err == nil {
			t.Fatal("expected error but got nil")
		}

		val, _ := s.Get("k1")
		if val != "v1" {
			t.Errorf("expected key to be restored, got %s", val)
		}
	})
}

func TestConfigService_GetAll(t *testing.T) {
	t.Parallel()
	repo := &mockConfigRepo{config: map[string]string{"k1": "v1", "k2": "v2"}}
	s := NewConfigService(repo)
	_ = s.Initialize(context.Background())

	all := s.GetAll()
	if len(all) != 2 {
		t.Errorf("expected 2 items, got %d", len(all))
	}
	if all["k1"] != "v1" || all["k2"] != "v2" {
		t.Errorf("unexpected data in GetAll: %v", all)
	}

	// Verify it returns a copy
	all["k1"] = "modified"
	val, _ := s.Get("k1")
	if val != "v1" {
		t.Error("GetAll returned a map that affects internal state when modified")
	}
}

func TestConfigService_Concurrency(t *testing.T) {
	t.Parallel()
	repo := &mockConfigRepo{config: make(map[string]string)}
	s := NewConfigService(repo)
	ctx := context.Background()

	const iterations = 100
	done := make(chan bool)

	go func() {
		for i := 0; i < iterations; i++ {
			_ = s.Set(ctx, fmt.Sprintf("key%d", i), "val")
		}
		done <- true
	}()

	go func() {
		for i := 0; i < iterations; i++ {
			_, _ = s.Get(fmt.Sprintf("key%d", i))
		}
		done <- true
	}()

	go func() {
		for i := 0; i < iterations; i++ {
			_ = s.GetAll()
		}
		done <- true
	}()

	for i := 0; i < 3; i++ {
		<-done
	}
}
