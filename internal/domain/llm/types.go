// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"context"
	"errors"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// Shared API models for LLM interactions.

type Content struct {
	Role       string  `json:"role"`
	Parts      []*Part `json:"parts,omitempty"`
	TokenCount int     `json:"token_count,omitempty"`
}

type Part struct {
	Text             string            `json:"text,omitempty"`
	InlineData       *Blob             `json:"inline_data,omitempty"`
	FunctionCall     *FunctionCall     `json:"function_call,omitempty"`
	FunctionResponse *FunctionResponse `json:"function_response,omitempty"`
	Thought          bool              `json:"thought,omitempty"`
	ThoughtSignature []byte            `json:"thought_signature,omitempty"`
	AssetID          string            `json:"asset_id,omitempty"` // Local reference for persistence
}

type Blob struct {
	MIMEType string `json:"mime_type,omitempty"`
	Data     []byte `json:"data,omitempty"`
}

type FunctionCall struct {
	Name string                 `json:"name,omitempty"`
	Args map[string]interface{} `json:"args,omitempty"`
}

type FunctionResponse struct {
	Name     string                 `json:"name,omitempty"`
	Response map[string]interface{} `json:"response,omitempty"`
}

// Metrics represents the token usage and timing for a single API turn.
type Metrics struct {
	Timestamp      string  `json:"timestamp"`
	Model          string  `json:"model,omitempty"`
	CachedTokens   int32   `json:"cached_tokens"`
	PromptTokens   int32   `json:"prompt_tokens"`
	ResponseTokens int32   `json:"response_tokens"`
	TotalTokens    int32   `json:"total_tokens"`
	ThinkingTokens int32   `json:"thinking_tokens,omitempty"`
	SearchQueries  int     `json:"search_queries,omitempty"`
	Duration       float64 `json:"duration"`
	ToolDuration   float64 `json:"tool_duration,omitempty"`
}

// ModelPricing represents the cost structure for a specific model tier.
type ModelPricing struct {
	Hit             float64 `json:"hit" yaml:"HIT"`
	Miss            float64 `json:"miss" yaml:"MISS"`
	Comp            float64 `json:"comp" yaml:"COMP"`
	TieredThreshold int64   `json:"tiered_threshold" yaml:"TIERED_THRESHOLD"`
	TieredMiss      float64 `json:"tiered_miss" yaml:"TIERED_MISS"`
	TieredComp      float64 `json:"tiered_comp" yaml:"TIERED_COMP"`
	ThinkingBudget  int     `json:"thinking_budget,omitempty" yaml:"THINKING_BUDGET,omitempty"`
}

// PricingData represents the global pricing information.
type PricingData struct {
	UpdatedAt       string                  `json:"updated_at"`
	Models          map[string]ModelPricing `json:"models"`
	ThinkingBudgets map[string]int          `json:"thinking_budgets,omitempty"`
	SearchQuery     float64                 `json:"search_query"`
}

// AssetResolver defines the interface for lazy hydration of binary assets.
type AssetResolver interface {
	Resolve(ctx context.Context, assetID string) ([]byte, error)
}

// LLMClient defines the interface for AI model providers.
type LLMClient interface {
	SendChat(ctx context.Context, history []*Content, tools []*tools.ToolDeclaration, resolver AssetResolver) (*Content, *Metrics, error)
	StreamChat(ctx context.Context, history []*Content, tools []*tools.ToolDeclaration, resolver AssetResolver, callback func(*Content)) (*Metrics, error)
	GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error)
	RefreshAuth() error
}

var (
	// ErrContextLimitExceeded is returned when the payload exceeds the safety threshold.
	ErrContextLimitExceeded = errors.New("payload estimate exceeds safety limit")

	// ErrMaxTurnsReached is returned when the model reaches the turn limit.
	ErrMaxTurnsReached = errors.New("maximum tool execution turns reached")

	// ErrBudgetExceeded is returned when the session cost exceeds the configured budget.
	ErrBudgetExceeded = errors.New("session budget exceeded")
)

// AddPart merges a new part into the content, appending or joining text parts as appropriate.
func (c *Content) AddPart(p *Part) {
	if p == nil {
		return
	}

	// If it's a function call/response, just append
	if p.FunctionCall != nil || p.FunctionResponse != nil || p.InlineData != nil {
		c.Parts = append(c.Parts, p)
		return
	}

	// For text/thought, try to append to last part if same type
	if len(c.Parts) > 0 {
		last := c.Parts[len(c.Parts)-1]
		if last.Thought == p.Thought && last.FunctionCall == nil && last.FunctionResponse == nil && last.InlineData == nil {
			last.Text += p.Text
			return
		}
	}

	// Otherwise append new part
	c.Parts = append(c.Parts, &Part{
		Text:    p.Text,
		Thought: p.Thought,
	})
}
