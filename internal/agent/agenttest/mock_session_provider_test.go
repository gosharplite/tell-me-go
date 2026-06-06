// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// stubKVStore is a minimal ports.KVStore for testing MockSessionProvider.
type stubKVStore struct{}

func (s *stubKVStore) Get(_ context.Context, _ string) (string, error)     { return "", nil }
func (s *stubKVStore) Set(_ context.Context, _ string, _ string) error     { return nil }
func (s *stubKVStore) Delete(_ context.Context, _ string) error            { return nil }
func (s *stubKVStore) GetAll(_ context.Context) (map[string]string, error) { return nil, nil }

// stubHealthChecker is a minimal ports.HealthChecker for testing MockSessionProvider.
type stubHealthChecker struct{}

func (s *stubHealthChecker) Check(_ context.Context) (*ports.ComponentReport, error) { return nil, nil }

func TestMockSessionProvider_GetTasks(t *testing.T) {
	t.Parallel()

	m := new(MockSessionProvider)
	got := m.GetTasks()
	if got != nil {
		t.Errorf("got %+v; want nil", got)
	}
}

func TestMockSessionProvider_GetSettings_Nil(t *testing.T) {
	t.Parallel()

	m := new(MockSessionProvider)
	m.On("GetSettings").Return(nil)

	got := m.GetSettings()
	if got != nil {
		t.Errorf("got %+v; want nil", got)
	}
	m.AssertExpectations(t)
}

func TestMockSessionProvider_GetSettings_NonNil(t *testing.T) {
	t.Parallel()

	store := &stubKVStore{}
	m := new(MockSessionProvider)
	m.On("GetSettings").Return(store)

	got := m.GetSettings()
	if got != store {
		t.Fatalf("got %+v; want %+v", got, store)
	}
	m.AssertExpectations(t)
}

func TestMockSessionProvider_GetInfo(t *testing.T) {
	t.Parallel()

	want := ports.SessionInfo{Model: "gpt-4"}
	m := new(MockSessionProvider)
	m.On("GetInfo").Return(want)

	got := m.GetInfo()
	if got.Model != want.Model {
		t.Errorf("got Model %q; want %q", got.Model, want.Model)
	}
	m.AssertExpectations(t)
}

func TestMockSessionProvider_SetInfo(t *testing.T) {
	t.Parallel()

	info := ports.SessionInfo{Provider: "openai"}
	m := new(MockSessionProvider)
	m.On("SetInfo", info).Return()

	m.SetInfo(info)
	m.AssertCalled(t, "SetInfo", info)
}

func TestMockSessionProvider_Close_Success(t *testing.T) {
	t.Parallel()

	m := new(MockSessionProvider)
	m.On("Close").Return(nil)

	err := m.Close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m.AssertExpectations(t)
}

func TestMockSessionProvider_Close_Error(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("close failed")
	m := new(MockSessionProvider)
	m.On("Close").Return(wantErr)

	err := m.Close()
	if !errors.Is(err, wantErr) {
		t.Fatalf("got error %v; want %v", err, wantErr)
	}
	m.AssertExpectations(t)
}

func TestMockSessionProvider_GetHealthChecker_Nil(t *testing.T) {
	t.Parallel()

	m := new(MockSessionProvider)
	m.On("GetHealthChecker").Return(nil)

	got := m.GetHealthChecker()
	if got != nil {
		t.Errorf("got %+v; want nil", got)
	}
	m.AssertExpectations(t)
}

func TestMockSessionProvider_GetHealthChecker_NonNil(t *testing.T) {
	t.Parallel()

	hc := &stubHealthChecker{}
	m := new(MockSessionProvider)
	m.On("GetHealthChecker").Return(hc)

	got := m.GetHealthChecker()
	if got != hc {
		t.Fatalf("got %+v; want %+v", got, hc)
	}
	m.AssertExpectations(t)
}
