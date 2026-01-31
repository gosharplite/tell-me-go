// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package types

import (
	"context"

	"google.golang.org/genai"
)

// Shared API models to decouple internal packages from internal/api.

type Content = genai.Content
type Part = genai.Part
type FunctionCall = genai.FunctionCall
type FunctionResponse = genai.FunctionResponse

// LLMClient defines the interface for AI model providers.
type LLMClient interface {
	SendChat(ctx context.Context, history []*Content, tools []*genai.Tool) (*Content, *Metrics, error)
	GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error)
	RefreshAuth() error
}

// Metrics represents the token usage and timing for a single API turn.
type Metrics struct {
	Timestamp      string
	CachedTokens   int32
	PromptTokens   int32
	ResponseTokens int32
	TotalTokens    int32
	ThinkingTokens int32
	SearchQueries  int
	Duration       float64
	ToolDuration   float64
}

// ToolResult represents the outcome of a tool execution.
type ToolResult struct {
	Text       string
	BinaryData []BinaryData
}

// BinaryData represents multi-modal content.
type BinaryData struct {
	MIMEType string
	Data     []byte
}
