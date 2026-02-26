// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type testContextKey string

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
	t.Run("WithContext", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), testContextKey("test"), "value")
		params := NewChatterParams(WithContext(ctx))
		assert.Equal(t, ctx, params.Context)
	})

	t.Run("WithLoader", func(t *testing.T) {
		var loader config.ConfigLoader
		params := NewChatterParams(WithLoader(loader))
		assert.Equal(t, loader, params.Loader)
	})

	t.Run("WithGateway", func(t *testing.T) {
		var gateway llm.LLMGateway
		params := NewChatterParams(WithGateway(gateway))
		assert.Equal(t, gateway, params.Gateway)
	})

	t.Run("WithHistory", func(t *testing.T) {
		var history HistoryManager
		params := NewChatterParams(WithHistory(history))
		assert.Equal(t, history, params.HistoryManager)
	})

	t.Run("WithToolConfig", func(t *testing.T) {
		var registry tools.IToolRegistry
		params := NewChatterParams(WithToolConfig(registry))
		assert.Equal(t, registry, params.Registry)
	})

	t.Run("WithSecurityManager", func(t *testing.T) {
		var securityManager security.ISecurityManager
		params := NewChatterParams(WithSecurityManager(securityManager))
		assert.Equal(t, securityManager, params.SecurityManager)
	})

	t.Run("WithStreamingDisabled", func(t *testing.T) {
		params := NewChatterParams(WithStreamingDisabled(true))
		assert.True(t, params.DisableStreaming)
	})

	t.Run("WithEventBus", func(t *testing.T) {
		var bus events.EventBus
		params := NewChatterParams(WithEventBus(bus))
		assert.Equal(t, bus, params.EventBus)
	})

	t.Run("WithProvider", func(t *testing.T) {
		params := NewChatterParams(WithProvider("test-provider"))
		assert.Equal(t, "test-provider", params.ProviderName)
	})

	t.Run("WithModel", func(t *testing.T) {
		params := NewChatterParams(WithModel("test-model"))
		assert.Equal(t, "test-model", params.Model)
	})

	t.Run("WithMode", func(t *testing.T) {
		params := NewChatterParams(WithMode("test-mode"))
		assert.Equal(t, "test-mode", params.Mode)
	})

	t.Run("WithLogPath", func(t *testing.T) {
		params := NewChatterParams(WithLogPath("/tmp/test.log"))
		assert.Equal(t, "/tmp/test.log", params.LogPath)
	})

	t.Run("WithPricingOverrides", func(t *testing.T) {
		overrides := map[string]pricing.ModelPricing{
			"test": {Miss: 1.0, Comp: 2.0},
		}
		params := NewChatterParams(WithPricingOverrides(overrides))
		assert.Equal(t, overrides, params.PricingOverrides)
	})

	t.Run("WithCostTracker", func(t *testing.T) {
		var tracker pricing.ICostTracker
		params := NewChatterParams(WithCostTracker(tracker))
		assert.Equal(t, tracker, params.CostTracker)
	})
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
