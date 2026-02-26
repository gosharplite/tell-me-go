// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// Summarizer defines the contract for compressing conversation history.
type Summarizer interface {
	// Summarize generates a concise text summary of the provided content slice.
	Summarize(ctx context.Context, contents []*llm.Content, focus string) (string, *llm.Metrics, error)
}
