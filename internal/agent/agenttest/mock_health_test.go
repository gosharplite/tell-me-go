// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

func TestMockHealthCheckManager_CheckAll_Success(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	want := &ports.HealthReport{OverallStatus: ports.StatusHealthy}

	m := new(MockHealthCheckManager)
	m.On("CheckAll", ctx).Return(want, nil)

	got, err := m.CheckAll(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Fatalf("got %+v; want %+v", got, want)
	}
	m.AssertExpectations(t)
}

func TestMockHealthCheckManager_CheckAll_Error(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	wantErr := errors.New("health check failed")

	m := new(MockHealthCheckManager)
	m.On("CheckAll", ctx).Return(nil, wantErr)

	got, err := m.CheckAll(ctx)
	if !errors.Is(err, wantErr) {
		t.Fatalf("got error %v; want %v", err, wantErr)
	}
	if got != nil {
		t.Fatalf("got %+v; want nil", got)
	}
	m.AssertExpectations(t)
}

func TestMockHealthCheckManager_CheckComponent_Success(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	comp := ports.CompLLMProvider
	want := &ports.ComponentReport{Component: comp, Status: ports.StatusHealthy}

	m := new(MockHealthCheckManager)
	m.On("CheckComponent", ctx, comp).Return(want, nil)

	got, err := m.CheckComponent(ctx, comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Fatalf("got %+v; want %+v", got, want)
	}
	m.AssertExpectations(t)
}

func TestMockHealthCheckManager_CheckComponent_Error(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	comp := ports.CompPersistence
	wantErr := errors.New("component check failed")

	m := new(MockHealthCheckManager)
	m.On("CheckComponent", ctx, comp).Return(nil, wantErr)

	got, err := m.CheckComponent(ctx, comp)
	if !errors.Is(err, wantErr) {
		t.Fatalf("got error %v; want %v", err, wantErr)
	}
	if got != nil {
		t.Fatalf("got %+v; want nil", got)
	}
	m.AssertExpectations(t)
}
