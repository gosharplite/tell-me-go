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

func TestMockHealthCheckManager_CheckAll_Success(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	want := &ports.HealthReport{OverallStatus: ports.StatusHealthy}

	m := &MockHealthCheckManager{
		CheckAllFunc: func(ctx context.Context) (*ports.HealthReport, error) {
			return want, nil
		},
	}

	got, err := m.CheckAll(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Fatalf("got %+v; want %+v", got, want)
	}

	ca, cc, _ := m.Snapshot()
	if ca != 1 {
		t.Errorf("CheckAll calls: got %d, want 1", ca)
	}
	if cc != 0 {
		t.Errorf("CheckComponent calls: got %d, want 0", cc)
	}
}

func TestMockHealthCheckManager_CheckAll_Error(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	wantErr := errors.New("health check failed")

	m := &MockHealthCheckManager{
		CheckAllFunc: func(ctx context.Context) (*ports.HealthReport, error) {
			return nil, wantErr
		},
	}

	got, err := m.CheckAll(ctx)
	if !errors.Is(err, wantErr) {
		t.Fatalf("got error %v; want %v", err, wantErr)
	}
	if got != nil {
		t.Fatalf("got %+v; want nil", got)
	}

	ca, cc, _ := m.Snapshot()
	if ca != 1 {
		t.Errorf("CheckAll calls: got %d, want 1", ca)
	}
	if cc != 0 {
		t.Errorf("CheckComponent calls: got %d, want 0", cc)
	}
}

func TestMockHealthCheckManager_CheckComponent_Success(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	comp := ports.CompLLMProvider
	want := &ports.ComponentReport{Component: comp, Status: ports.StatusHealthy}

	m := &MockHealthCheckManager{
		CheckComponentFunc: func(ctx context.Context, c ports.Component) (*ports.ComponentReport, error) {
			return want, nil
		},
	}

	got, err := m.CheckComponent(ctx, comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Fatalf("got %+v; want %+v", got, want)
	}

	ca, cc, _ := m.Snapshot()
	if ca != 0 {
		t.Errorf("CheckAll calls: got %d, want 0", ca)
	}
	if cc != 1 {
		t.Errorf("CheckComponent calls: got %d, want 1", cc)
	}
}

func TestMockHealthCheckManager_CheckComponent_Error(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	comp := ports.CompPersistence
	wantErr := errors.New("component check failed")

	m := &MockHealthCheckManager{
		CheckComponentFunc: func(ctx context.Context, c ports.Component) (*ports.ComponentReport, error) {
			return nil, wantErr
		},
	}

	got, err := m.CheckComponent(ctx, comp)
	if !errors.Is(err, wantErr) {
		t.Fatalf("got error %v; want %v", err, wantErr)
	}
	if got != nil {
		t.Fatalf("got %+v; want nil", got)
	}

	ca, cc, _ := m.Snapshot()
	if ca != 0 {
		t.Errorf("CheckAll calls: got %d, want 0", ca)
	}
	if cc != 1 {
		t.Errorf("CheckComponent calls: got %d, want 1", cc)
	}
}

func TestMockHealthCheckManager_RaceCondition(t *testing.T) {
	// 5 goroutines × 20 iterations, mixing CheckAll + CheckComponent
	m := &MockHealthCheckManager{
		CheckAllFunc: func(ctx context.Context) (*ports.HealthReport, error) {
			return &ports.HealthReport{OverallStatus: ports.StatusHealthy}, nil
		},
		CheckComponentFunc: func(ctx context.Context, comp ports.Component) (*ports.ComponentReport, error) {
			return &ports.ComponentReport{Component: comp, Status: ports.StatusHealthy}, nil
		},
	}

	ctx := context.Background()
	var wg sync.WaitGroup
	const goroutines = 5
	const iterations = 20

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if j%2 == 0 {
					_, _ = m.CheckAll(ctx)
				} else {
					_, _ = m.CheckComponent(ctx, ports.CompLLMProvider)
				}
			}
		}()
	}
	wg.Wait()

	ca, cc, methods := m.Snapshot()

	expectedAll := goroutines * iterations / 2 // even iterations
	expectedComp := goroutines*iterations - expectedAll

	if ca != expectedAll {
		t.Errorf("CheckAll calls: got %d, want %d", ca, expectedAll)
	}
	if cc != expectedComp {
		t.Errorf("CheckComponent calls: got %d, want %d", cc, expectedComp)
	}
	if len(methods) != goroutines*iterations {
		t.Errorf("calledMethods length: got %d, want %d", len(methods), goroutines*iterations)
	}
}
