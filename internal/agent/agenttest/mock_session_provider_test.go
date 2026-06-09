// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
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

	var trackedCalled bool
	m := &MockSessionProvider{
		GetSettingsFn: func() ports.KVStore { trackedCalled = true; return nil },
	}
	got := m.GetTasks()
	if got != nil {
		t.Errorf("got %+v; want nil", got)
	}
	if trackedCalled {
		t.Error("GetTasks should not trigger any tracked method")
	}
}

func TestMockSessionProvider_GetSettings_Nil(t *testing.T) {
	t.Parallel()

	var called bool
	m := &MockSessionProvider{
		GetSettingsFn: func() ports.KVStore { called = true; return nil },
	}
	got := m.GetSettings()
	if got != nil {
		t.Errorf("got %+v; want nil", got)
	}
	if !called {
		t.Error("GetSettingsFn was not called")
	}
}

func TestMockSessionProvider_GetSettings_NonNil(t *testing.T) {
	t.Parallel()

	store := &stubKVStore{}
	var called bool
	m := &MockSessionProvider{
		GetSettingsFn: func() ports.KVStore { called = true; return store },
	}
	got := m.GetSettings()
	if got != store {
		t.Fatalf("got %+v; want %+v", got, store)
	}
	if !called {
		t.Error("GetSettingsFn was not called")
	}
}

func TestMockSessionProvider_GetInfo(t *testing.T) {
	t.Parallel()

	want := ports.SessionInfo{Model: "gpt-4"}
	var called bool
	m := &MockSessionProvider{
		GetInfoFn: func() ports.SessionInfo { called = true; return want },
	}
	got := m.GetInfo()
	if got.Model != want.Model {
		t.Errorf("got Model %q; want %q", got.Model, want.Model)
	}
	if !called {
		t.Error("GetInfoFn was not called")
	}
}

func TestMockSessionProvider_SetInfo(t *testing.T) {
	t.Parallel()

	var captured ports.SessionInfo
	info := ports.SessionInfo{Provider: "openai"}
	var called bool
	m := &MockSessionProvider{
		SetInfoFn: func(in ports.SessionInfo) { called = true; captured = in },
	}
	m.SetInfo(info)
	if captured.Provider != info.Provider {
		t.Errorf("captured Provider %q; want %q", captured.Provider, info.Provider)
	}
	if !called {
		t.Error("SetInfoFn was not called")
	}
}

func TestMockSessionProvider_Close_Success(t *testing.T) {
	t.Parallel()

	var called bool
	m := &MockSessionProvider{
		CloseFn: func() error { called = true; return nil },
	}
	err := m.Close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("CloseFn was not called")
	}
}

func TestMockSessionProvider_Close_Error(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("close failed")
	var called bool
	m := &MockSessionProvider{
		CloseFn: func() error { called = true; return wantErr },
	}
	err := m.Close()
	if !errors.Is(err, wantErr) {
		t.Fatalf("got error %v; want %v", err, wantErr)
	}
	if !called {
		t.Error("CloseFn was not called")
	}
}

func TestMockSessionProvider_GetHealthChecker_Nil(t *testing.T) {
	t.Parallel()

	var called bool
	m := &MockSessionProvider{
		GetHealthCheckerFn: func() ports.HealthChecker { called = true; return nil },
	}
	got := m.GetHealthChecker()
	if got != nil {
		t.Errorf("got %+v; want nil", got)
	}
	if !called {
		t.Error("GetHealthCheckerFn was not called")
	}
}

func TestMockSessionProvider_GetHealthChecker_NonNil(t *testing.T) {
	t.Parallel()

	hc := &stubHealthChecker{}
	var called bool
	m := &MockSessionProvider{
		GetHealthCheckerFn: func() ports.HealthChecker { called = true; return hc },
	}
	got := m.GetHealthChecker()
	if got != hc {
		t.Fatalf("got %+v; want %+v", got, hc)
	}
	if !called {
		t.Error("GetHealthCheckerFn was not called")
	}
}

func TestMockSessionProvider_RaceCondition(t *testing.T) {
	// 5 goroutines × 20 iterations calling all 5 tracked methods.
	// -race must not detect any data race.
	t.Parallel()

	var total atomic.Int32
	m := &MockSessionProvider{
		GetSettingsFn:      func() ports.KVStore { total.Add(1); return &stubKVStore{} },
		GetInfoFn:          func() ports.SessionInfo { total.Add(1); return ports.SessionInfo{Model: "r"} },
		SetInfoFn:          func(_ ports.SessionInfo) { total.Add(1) },
		CloseFn:            func() error { total.Add(1); return nil },
		GetHealthCheckerFn: func() ports.HealthChecker { total.Add(1); return &stubHealthChecker{} },
	}

	var wg sync.WaitGroup
	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				_ = m.GetSettings()
				_ = m.GetInfo()
				m.SetInfo(ports.SessionInfo{})
				_ = m.Close()
				_ = m.GetHealthChecker()
			}
		}()
	}
	wg.Wait()

	want := int32(5 * 20 * 5)
	if got := total.Load(); got != want {
		t.Errorf("total calls: got %d, want %d", got, want)
	}
}
