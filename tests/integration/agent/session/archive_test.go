// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/testutil"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
)

func TestSummarizeRange_Archival(t *testing.T) {
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "history.jsonl")
	archiveFile := filepath.Join(tmpDir, "history.archive.jsonl")

	hManager := history.NewManager(persistencetest.NewPlainOSFileSystem(), historyFile, archiveFile)
	ctx := context.Background()

	// 1. Add some history (2 turns = 4 messages)
	_ = hManager.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "Turn 1 User"}}})
	_ = hManager.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Turn 1 Model"}}})
	_ = hManager.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "Turn 2 User"}}})
	_ = hManager.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Turn 2 Model"}}})

	tc := &testutil.MockTokenCounter{}
	tc.SetTokens(100)
	strategy := session.NewContextStrategy(tc)

	cm := session.NewContextManager(strategy, hManager, nil, nil)
	ms := &testutil.MockSummarizer{}
	ms.SetSummarizeFn(func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
		return "Summary of history", &llm.Metrics{}, nil
	})
	cm.Summarizer = ms

	// 2. Summarize all available turns (will summarize total-1 turns = 1 turn because of clamping in prepareSummarizationMetadata)
	// Actually, prepareSummarizationMetadata says:
	// if numTurns >= totalTurns { numTurns = totalTurns - 1 }
	// Total turns is 2. So numTurns becomes 1.

	// Let's add more turns to see reduction.
	_ = hManager.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "Turn 3 User"}}})
	_ = hManager.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Turn 3 Model"}}})
	// Total turns: 3 (6 messages).

	// Summarize 2 turns (4 messages).
	_, _, err := cm.SummarizeRange(ctx, 2, "")
	if err != nil {
		t.Fatalf("SummarizeRange failed: %v", err)
	}

	// 3. Verify archival
	if _, err := os.Stat(archiveFile); os.IsNotExist(err) {
		t.Fatal("archive file was not created after summarization")
	}

	// Load archive and verify contents
	archiveStore := history.NewManager(persistencetest.NewPlainOSFileSystem(), archiveFile, archiveFile+".archive")
	if err := archiveStore.Load(ctx); err != nil {
		t.Fatalf("failed to load archive: %v", err)
	}

	archived, _ := archiveStore.GetWindow(ctx, 0, -1)
	if len(archived) != 4 {
		t.Fatalf("expected 4 archived messages, got %d", len(archived))
	}

	if archived[0].Parts[0].Text != "Turn 1 User" {
		t.Errorf("expected 'Turn 1 User', got %q", archived[0].Parts[0].Text)
	}

	// 4. Verify main history
	mainContents, _ := hManager.GetWindow(ctx, 0, -1)
	// Should have: SummaryMsg(2 messages) + Turn 3 (2 messages) = 4 messages
	if len(mainContents) != 4 {
		t.Fatalf("expected 4 messages in main history, got %d", len(mainContents))
	}

	if mainContents[0].Role != "user" || !strings.Contains(mainContents[0].Parts[0].Text, "Summary of history") {
		t.Errorf("expected summary message at index 0, got %v", mainContents[0])
	}
}
