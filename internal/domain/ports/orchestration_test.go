// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

func TestNewSession(t *testing.T) {
	t.Parallel()
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

func TestChatterConfig(t *testing.T) {
	t.Parallel()
	cfg := ChatterConfig{
		ProviderName: "test-provider",
		Model:        "test-model",
		Mode:         "test-mode",
		LogPath:      "/tmp/test.log",
	}
	assert.Equal(t, "test-provider", cfg.ProviderName)
	assert.Equal(t, "test-model", cfg.Model)
	assert.Equal(t, "test-mode", cfg.Mode)
	assert.Equal(t, "/tmp/test.log", cfg.LogPath)
}

func TestContextMetadata_Clone(t *testing.T) {
	t.Parallel()
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
