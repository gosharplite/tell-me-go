// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package context

import (
	"context"
	"fmt"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsTokenCountSafe(t *testing.T) {
	tests := []struct {
		name     string
		tokens   int
		window   int
		expected bool
	}{
		{"Safe", 500, 1000, true},
		{"ExactlyLimit", 900, 1000, true},
		{"Unsafe", 901, 1000, false},
		{"Large", 10000, 100000, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			safe, limit := isTokenCountSafe(tt.tokens, tt.window)
			assert.Equal(t, tt.expected, safe)
			assert.Equal(t, int(float64(tt.window)*0.9), limit)
		})
	}
}

// makePinnableTurns creates N turns (each turn = user + model) as []*llm.Content.
// pinnedIndices contains zero-based turn indices whose messages will have Pinned=true.
func makePinnableTurns(n int, pinnedIndices ...int) []*llm.Content {
	pinned := make(map[int]bool)
	for _, idx := range pinnedIndices {
		pinned[idx] = true
	}
	var contents []*llm.Content
	for i := 0; i < n; i++ {
		c1 := &llm.Content{Role: "user", Parts: []*llm.Part{{Text: fmt.Sprintf("u%d", i+1)}}, Pinned: pinned[i]}
		c2 := &llm.Content{Role: "model", Parts: []*llm.Part{{Text: fmt.Sprintf("m%d", i+1)}}, Pinned: pinned[i]}
		contents = append(contents, c1, c2)
	}
	return contents
}

func TestFindSummarizationBoundary_PinnedAware(t *testing.T) {
	tests := []struct {
		name         string
		contents     []*llm.Content
		numTurns     int
		wantStartIdx int
		wantEndIdx   int
		wantNil      bool
	}{
		{
			name:         "all unpinned, request 2",
			contents:     makePinnableTurns(4), // 4 turns, all unpinned
			numTurns:     2,
			wantStartIdx: 0,
			wantEndIdx:   4, // turns 0+1 → messages [0:4]
		},
		{
			name:         "first turn pinned, skips it",
			contents:     makePinnableTurns(4, 0), // turn 0 pinned
			numTurns:     2,
			wantStartIdx: 2, // skip turn 0, use turns 1+2
			wantEndIdx:   6,
		},
		{
			name:         "interspersed pins, skips pinned",
			contents:     makePinnableTurns(5, 0, 2), // turns 0 and 2 pinned
			numTurns:     2,
			wantStartIdx: 6, // skip pinned, use turns 3+4
			wantEndIdx:   10,
		},
		{
			name:     "all pinned, no viable block",
			contents: makePinnableTurns(4, 0, 1, 2, 3),
			numTurns: 2,
			wantNil:  true,
		},
		{
			name:     "single unpinned, request exceeds available",
			contents: makePinnableTurns(2, 0), // turn 0 pinned, turn 1 unpinned
			numTurns: 3,
			wantNil:  true, // best block count=1 < MinViableBlock=2
		},
		{
			name:     "empty history",
			contents: nil,
			numTurns: 2,
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := &agenttest.MockTokenCounter{}
			strategy := NewStrategy(tc)
			history := &agenttest.MockHistoryManager{}
			history.SetInternalContents(tt.contents)
			cm := NewManager(strategy, history, nil, nil)

			ctx := context.Background()
			totalEntries := len(tt.contents)

			subset, startIdx, endIdx, err := cm.findSummarizationBoundary(ctx, tt.numTurns, totalEntries)
			require.NoError(t, err)

			if tt.wantNil {
				assert.Nil(t, subset)
				return
			}

			require.NotNil(t, subset)
			assert.Equal(t, tt.wantStartIdx, startIdx, "startIdx mismatch")
			assert.Equal(t, tt.wantEndIdx, endIdx, "endIdx mismatch")
			assert.Len(t, subset, tt.wantEndIdx-tt.wantStartIdx, "subset length mismatch")
		})
	}
}

func TestFindSummarizationBoundary_WindowGrowth(t *testing.T) {
	// 20 messages: first 16 are pinned (8 turns), last 4 are unpinned (2 turns).
	// Requesting 2 turns → initial window of 8 msgs only sees pinned → window grows
	// until it reaches 20, finally discovering the 2 unpinned turns at the end.
	var contents []*llm.Content
	for i := 0; i < 8; i++ {
		contents = append(contents,
			&llm.Content{Role: "user", Parts: []*llm.Part{{Text: fmt.Sprintf("pu%d", i+1)}}, Pinned: true},
			&llm.Content{Role: "model", Parts: []*llm.Part{{Text: fmt.Sprintf("pm%d", i+1)}}, Pinned: true},
		)
	}
	for i := 0; i < 2; i++ {
		contents = append(contents,
			&llm.Content{Role: "user", Parts: []*llm.Part{{Text: fmt.Sprintf("u%d", i+1)}}},
			&llm.Content{Role: "model", Parts: []*llm.Part{{Text: fmt.Sprintf("m%d", i+1)}}},
		)
	}

	tc := &agenttest.MockTokenCounter{}
	strategy := NewStrategy(tc)
	history := &agenttest.MockHistoryManager{}
	history.SetInternalContents(contents)
	cm := NewManager(strategy, history, nil, nil)

	ctx := context.Background()
	subset, startIdx, endIdx, err := cm.findSummarizationBoundary(ctx, 2, len(contents))
	require.NoError(t, err)
	require.NotNil(t, subset)

	// Expect the last 2 unpinned turns (messages [16:20]).
	assert.Equal(t, 16, startIdx)
	assert.Equal(t, 20, endIdx)
	assert.Len(t, subset, 4)
}
