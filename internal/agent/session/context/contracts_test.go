// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package context

import (
	"reflect"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

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

	cloned := original.clone()

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
