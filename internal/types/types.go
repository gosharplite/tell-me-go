// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package types

import (
	"context"

	"google.golang.org/genai"
)

// Shared API models to decouple internal packages from genai.

type Content struct {
	Role  string  `json:"role,omitempty"`
	Parts []*Part `json:"parts,omitempty"`
}

type Part struct {
	Text             string            `json:"text,omitempty"`
	InlineData       *genai.Blob       `json:"inline_data,omitempty"`
	FunctionCall     *genai.FunctionCall `json:"function_call,omitempty"`
	FunctionResponse *genai.FunctionResponse `json:"function_response,omitempty"`
	Thought          bool              `json:"thought,omitempty"`
	AssetID          string            `json:"asset_id,omitempty"` // Local reference for persistence
}

type FunctionCall = genai.FunctionCall
type FunctionResponse = genai.FunctionResponse

// ToSDK converts internal Content to genai.Content.
func (c *Content) ToSDK() *genai.Content {
	if c == nil {
		return nil
	}
	res := &genai.Content{
		Role: c.Role,
	}
	for _, p := range c.Parts {
		res.Parts = append(res.Parts, p.ToSDK())
	}
	return res
}

// ToSDK converts internal Part to genai.Part.
func (p *Part) ToSDK() *genai.Part {
	if p == nil {
		return nil
	}
	return &genai.Part{
		Text:             p.Text,
		InlineData:       p.InlineData,
		FunctionCall:     p.FunctionCall,
		FunctionResponse: p.FunctionResponse,
		Thought:          p.Thought,
	}
}

// FromSDKContent converts genai.Content to internal Content.
func FromSDKContent(c *genai.Content) *Content {
	if c == nil {
		return nil
	}
	res := &Content{
		Role: c.Role,
	}
	for _, p := range c.Parts {
		res.Parts = append(res.Parts, FromSDKPart(p))
	}
	return res
}

// FromSDKPart converts genai.Part to internal Part.
func FromSDKPart(p *genai.Part) *Part {
	if p == nil {
		return nil
	}
	return &Part{
		Text:             p.Text,
		InlineData:       p.InlineData,
		FunctionCall:     p.FunctionCall,
		FunctionResponse: p.FunctionResponse,
		Thought:          p.Thought,
	}
}

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
