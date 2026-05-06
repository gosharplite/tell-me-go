// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package context

import "github.com/gosharplite/tell-me-go/internal/domain/llm"

// CandidateSelector defines a strategy for identifying which history turns
// are eligible for summarization. Implementations are stateless — the caller
// handles context cancellation and iteration.
type CandidateSelector interface {
	// IsCandidate reports whether the given turn is eligible for inclusion
	// in a summarization block.
	IsCandidate(turn []*llm.Content) bool

	// MinViableBlock returns the minimum number of turns required to form
	// a usable summarization block.
	MinViableBlock() int
}

// ContiguousUnpinnedSelector implements CandidateSelector by selecting
// turns whose messages are all unpinned. The minimum viable block size is 2.
type ContiguousUnpinnedSelector struct{}

func (s *ContiguousUnpinnedSelector) IsCandidate(turn []*llm.Content) bool {
	return !isTurnPinned(turn)
}

func (s *ContiguousUnpinnedSelector) MinViableBlock() int {
	return 2
}
