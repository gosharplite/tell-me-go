// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

func TestNewSession(t *testing.T) {
	id := "test-session"
	session := NewSession(id, nil)

	if session.ID != id {
		t.Errorf("expected session ID %s, got %s", id, session.ID)
	}
	if session.StartTime.IsZero() {
		t.Error("expected session StartTime to be set")
	}
	if time.Since(session.StartTime) > time.Second {
		t.Error("expected session StartTime to be recent")
	}
}

func TestChatterOptions(t *testing.T) {
	ctx := context.WithValue(context.Background(), "test", "value")
	loader := (config.ConfigLoader)(nil)
	gateway := (llm.LLMGateway)(nil)
	history := (HistoryManager)(nil)
	registry := (tools.IToolRegistry)(nil)
	securityManager := (security.ISecurityManager)(nil)
	bus := (events.EventBus)(nil)
	tracker := (pricing.ICostTracker)(nil)
	overrides := map[string]pricing.ModelPricing{
		"test": {Miss: 1.0, Comp: 2.0},
	}

	tests := []struct {
		name     string
		option   ChatterOption
		validate func(*testing.T, ChatterParams)
	}{
		{
			name:   "WithContext",
			option: WithContext(ctx),
			validate: func(t *testing.T, p ChatterParams) {
				if p.Context != ctx {
					t.Errorf("WithContext: expected %v, got %v", ctx, p.Context)
				}
			},
		},
		{
			name:   "WithLoader",
			option: WithLoader(loader),
			validate: func(t *testing.T, p ChatterParams) {
				if p.Loader != loader {
					t.Errorf("WithLoader: expected %v, got %v", loader, p.Loader)
				}
			},
		},
		{
			name:   "WithGateway",
			option: WithGateway(gateway),
			validate: func(t *testing.T, p ChatterParams) {
				if p.Gateway != gateway {
					t.Errorf("WithGateway: expected %v, got %v", gateway, p.Gateway)
				}
			},
		},
		{
			name:   "WithHistory",
			option: WithHistory(history),
			validate: func(t *testing.T, p ChatterParams) {
				if p.HistoryManager != history {
					t.Errorf("WithHistory: expected %v, got %v", history, p.HistoryManager)
				}
			},
		},
		{
			name:   "WithToolConfig",
			option: WithToolConfig(registry),
			validate: func(t *testing.T, p ChatterParams) {
				if p.Registry != registry {
					t.Errorf("WithToolConfig: expected %v, got %v", registry, p.Registry)
				}
			},
		},
		{
			name:   "WithSecurityManager",
			option: WithSecurityManager(securityManager),
			validate: func(t *testing.T, p ChatterParams) {
				if p.SecurityManager != securityManager {
					t.Errorf("WithSecurityManager: expected %v, got %v", securityManager, p.SecurityManager)
				}
			},
		},
		{
			name:   "WithStreamingDisabled",
			option: WithStreamingDisabled(true),
			validate: func(t *testing.T, p ChatterParams) {
				if !p.DisableStreaming {
					t.Errorf("WithStreamingDisabled: expected true, got false")
				}
			},
		},
		{
			name:   "WithEventBus",
			option: WithEventBus(bus),
			validate: func(t *testing.T, p ChatterParams) {
				if p.EventBus != bus {
					t.Errorf("WithEventBus: expected %v, got %v", bus, p.EventBus)
				}
			},
		},
		{
			name:   "WithProvider",
			option: WithProvider("test-provider"),
			validate: func(t *testing.T, p ChatterParams) {
				if p.ProviderName != "test-provider" {
					t.Errorf("WithProvider: expected test-provider, got %s", p.ProviderName)
				}
			},
		},
		{
			name:   "WithModel",
			option: WithModel("test-model"),
			validate: func(t *testing.T, p ChatterParams) {
				if p.Model != "test-model" {
					t.Errorf("WithModel: expected test-model, got %s", p.Model)
				}
			},
		},
		{
			name:   "WithMode",
			option: WithMode("test-mode"),
			validate: func(t *testing.T, p ChatterParams) {
				if p.Mode != "test-mode" {
					t.Errorf("WithMode: expected test-mode, got %s", p.Mode)
				}
			},
		},
		{
			name:   "WithLogPath",
			option: WithLogPath("/tmp/test.log"),
			validate: func(t *testing.T, p ChatterParams) {
				if p.LogPath != "/tmp/test.log" {
					t.Errorf("WithLogPath: expected /tmp/test.log, got %s", p.LogPath)
				}
			},
		},
		{
			name:   "WithPricingOverrides",
			option: WithPricingOverrides(overrides),
			validate: func(t *testing.T, p ChatterParams) {
				if !reflect.DeepEqual(p.PricingOverrides, overrides) {
					t.Errorf("WithPricingOverrides: expected %v, got %v", overrides, p.PricingOverrides)
				}
			},
		},
		{
			name:   "WithCostTracker",
			option: WithCostTracker(tracker),
			validate: func(t *testing.T, p ChatterParams) {
				if p.CostTracker != tracker {
					t.Errorf("WithCostTracker: expected %v, got %v", tracker, p.CostTracker)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := NewChatterParams(tt.option)
			tt.validate(t, params)
		})
	}
}

func TestContextMetadata_Clone(t *testing.T) {
	original := &ContextMetadata{
		OriginalTokenCount:     100,
		FinalTokenCount:        80,
		FinalTurnCount:         5,
		PrunedTurns:            2,
		SummarizedTurns:        1,
		SummarizationAttempted: true,
		MaintenanceBlocked:     false,
		Warnings:               []string{"warning1", "warning2"},
		TotalTurnsKept:         3,
		KeptByPolicy:           map[string]int{"policy1": 3},
		History: []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
		},
	}

	cloned := original.Clone()

	if !reflect.DeepEqual(original, cloned) {
		t.Errorf("expected cloned to be equal to original")
	}

	// Verify deep copy of slices and maps
	cloned.Warnings[0] = "changed"
	if original.Warnings[0] == "changed" {
		t.Error("expected Warnings to be deep copied")
	}

	cloned.KeptByPolicy["policy1"] = 99
	if original.KeptByPolicy["policy1"] == 99 {
		t.Error("expected KeptByPolicy to be deep copied")
	}

	cloned.History[0].Role = "assistant"
	if original.History[0].Role == "assistant" {
		t.Error("expected History to be deep copied")
	}
}
