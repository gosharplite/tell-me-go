// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"errors"
	"sync"
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

	m := &MockSessionProvider{}
	got := m.GetTasks()
	if got != nil {
		t.Errorf("got %+v; want nil", got)
	}
	// GetTasks is not tracked by spy counters.
	gs, gi, si, cl, ghc, methods := m.Snapshot()
	if gs != 0 {
		t.Errorf("GetSettings calls: got %d, want 0", gs)
	}
	if gi != 0 {
		t.Errorf("GetInfo calls: got %d, want 0", gi)
	}
	if si != 0 {
		t.Errorf("SetInfo calls: got %d, want 0", si)
	}
	if cl != 0 {
		t.Errorf("Close calls: got %d, want 0", cl)
	}
	if ghc != 0 {
		t.Errorf("GetHealthChecker calls: got %d, want 0", ghc)
	}
	if len(methods) != 0 {
		t.Errorf("methods: got %v, want empty", methods)
	}
}

func TestMockSessionProvider_GetSettings_Nil(t *testing.T) {
	t.Parallel()

	m := &MockSessionProvider{
		GetSettingsFn: func() ports.KVStore { return nil },
	}
	got := m.GetSettings()
	if got != nil {
		t.Errorf("got %+v; want nil", got)
	}
	gs, gi, si, cl, ghc, _ := m.Snapshot()
	if gs != 1 {
		t.Errorf("GetSettings calls: got %d, want 1", gs)
	}
	if gi != 0 {
		t.Errorf("GetInfo calls: got %d, want 0", gi)
	}
	if si != 0 {
		t.Errorf("SetInfo calls: got %d, want 0", si)
	}
	if cl != 0 {
		t.Errorf("Close calls: got %d, want 0", cl)
	}
	if ghc != 0 {
		t.Errorf("GetHealthChecker calls: got %d, want 0", ghc)
	}
}

func TestMockSessionProvider_GetSettings_NonNil(t *testing.T) {
	t.Parallel()

	store := &stubKVStore{}
	m := &MockSessionProvider{
		GetSettingsFn: func() ports.KVStore { return store },
	}
	got := m.GetSettings()
	if got != store {
		t.Fatalf("got %+v; want %+v", got, store)
	}
	gs, _, _, _, _, _ := m.Snapshot()
	if gs != 1 {
		t.Errorf("GetSettings calls: got %d, want 1", gs)
	}
}

func TestMockSessionProvider_GetInfo(t *testing.T) {
	t.Parallel()

	want := ports.SessionInfo{Model: "gpt-4"}
	m := &MockSessionProvider{
		GetInfoFn: func() ports.SessionInfo { return want },
	}
	got := m.GetInfo()
	if got.Model != want.Model {
		t.Errorf("got Model %q; want %q", got.Model, want.Model)
	}
	_, gi, _, _, _, _ := m.Snapshot()
	if gi != 1 {
		t.Errorf("GetInfo calls: got %d, want 1", gi)
	}
}

func TestMockSessionProvider_SetInfo(t *testing.T) {
	t.Parallel()

	var captured ports.SessionInfo
	info := ports.SessionInfo{Provider: "openai"}
	m := &MockSessionProvider{
		SetInfoFn: func(in ports.SessionInfo) { captured = in },
	}
	m.SetInfo(info)
	if captured.Provider != info.Provider {
		t.Errorf("captured Provider %q; want %q", captured.Provider, info.Provider)
	}
	_, _, si, _, _, _ := m.Snapshot()
	if si != 1 {
		t.Errorf("SetInfo calls: got %d, want 1", si)
	}
}

func TestMockSessionProvider_Close_Success(t *testing.T) {
	t.Parallel()

	m := &MockSessionProvider{
		CloseFn: func() error { return nil },
	}
	err := m.Close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, _, _, cl, _, _ := m.Snapshot()
	if cl != 1 {
		t.Errorf("Close calls: got %d, want 1", cl)
	}
}

func TestMockSessionProvider_Close_Error(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("close failed")
	m := &MockSessionProvider{
		CloseFn: func() error { return wantErr },
	}
	err := m.Close()
	if !errors.Is(err, wantErr) {
		t.Fatalf("got error %v; want %v", err, wantErr)
	}
	_, _, _, cl, _, _ := m.Snapshot()
	if cl != 1 {
		t.Errorf("Close calls: got %d, want 1", cl)
	}
}

func TestMockSessionProvider_GetHealthChecker_Nil(t *testing.T) {
	t.Parallel()

	m := &MockSessionProvider{
		GetHealthCheckerFn: func() ports.HealthChecker { return nil },
	}
	got := m.GetHealthChecker()
	if got != nil {
		t.Errorf("got %+v; want nil", got)
	}
	gs, gi, si, cl, ghc, _ := m.Snapshot()
	if gs != 0 {
		t.Errorf("GetSettings calls: got %d, want 0", gs)
	}
	if gi != 0 {
		t.Errorf("GetInfo calls: got %d, want 0", gi)
	}
	if si != 0 {
		t.Errorf("SetInfo calls: got %d, want 0", si)
	}
	if cl != 0 {
		t.Errorf("Close calls: got %d, want 0", cl)
	}
	if ghc != 1 {
		t.Errorf("GetHealthChecker calls: got %d, want 1", ghc)
	}
}

func TestMockSessionProvider_GetHealthChecker_NonNil(t *testing.T) {
	t.Parallel()

	hc := &stubHealthChecker{}
	m := &MockSessionProvider{
		GetHealthCheckerFn: func() ports.HealthChecker { return hc },
	}
	got := m.GetHealthChecker()
	if got != hc {
		t.Fatalf("got %+v; want %+v", got, hc)
	}
	_, _, _, _, ghc, _ := m.Snapshot()
	if ghc != 1 {
		t.Errorf("GetHealthChecker calls: got %d, want 1", ghc)
	}
}

func TestMockSessionProvider_RaceCondition(t *testing.T) {
	// 5 goroutines × 20 iterations calling all 5 tracked methods.
	// -race must not detect any data race.
	t.Parallel()

	m := &MockSessionProvider{
		GetSettingsFn:      func() ports.KVStore { return &stubKVStore{} },
		GetInfoFn:          func() ports.SessionInfo { return ports.SessionInfo{Model: "r"} },
		SetInfoFn:          func(_ ports.SessionInfo) {},
		CloseFn:            func() error { return nil },
		GetHealthCheckerFn: func() ports.HealthChecker { return &stubHealthChecker{} },
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

	gs, gi, si, cl, ghc, _ := m.Snapshot()
	total := gs + gi + si + cl + ghc
	want := 5 * 20 * 5 // 5 goroutines × 20 iterations × 5 methods
	if total != want {
		t.Errorf("total calls: got %d, want %d", total, want)
	}
}
