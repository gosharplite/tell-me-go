// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"bytes"
	"context"
	"errors"
	"reflect"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// Shared API models for LLM interactions.

type Content struct {
	Role           string  `json:"role"`
	Parts          []*Part `json:"parts,omitempty"`
	TokenCount     int     `json:"token_count,omitempty"`
	Pinned         bool    `json:"pinned,omitempty"`
	TransientParts []*Part `json:"-"`
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
	Cost           float64 `json:"cost,omitempty"` // USD cost for this turn or summary
	IsSummary      bool    `json:"is_summary,omitempty"`
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
	SetSystemInstructions(instr string)
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

// Clone returns a deep copy of Content.
func (c *Content) Clone() *Content {
	if c == nil {
		return nil
	}
	clone := &Content{
		Role:       c.Role,
		TokenCount: c.TokenCount,
		Pinned:     c.Pinned,
	}
	if c.Parts != nil {
		clone.Parts = make([]*Part, len(c.Parts))
		for i, p := range c.Parts {
			clone.Parts[i] = p.Clone()
		}
	}
	if c.TransientParts != nil {
		clone.TransientParts = make([]*Part, len(c.TransientParts))
		for i, p := range c.TransientParts {
			clone.TransientParts[i] = p.Clone()
		}
	}
	return clone
}

// Clone returns a deep copy of Part.
func (p *Part) Clone() *Part {
	if p == nil {
		return nil
	}
	clone := &Part{
		Text:             p.Text,
		Thought:          p.Thought,
		AssetID:          p.AssetID,
		InlineData:       p.InlineData.Clone(),
		FunctionCall:     p.FunctionCall.Clone(),
		FunctionResponse: p.FunctionResponse.Clone(),
	}
	if p.ThoughtSignature != nil {
		clone.ThoughtSignature = make([]byte, len(p.ThoughtSignature))
		copy(clone.ThoughtSignature, p.ThoughtSignature)
	}
	return clone
}

// Clone returns a deep copy of Blob.
func (b *Blob) Clone() *Blob {
	if b == nil {
		return nil
	}
	clone := &Blob{
		MIMEType: b.MIMEType,
	}
	if b.Data != nil {
		clone.Data = make([]byte, len(b.Data))
		copy(clone.Data, b.Data)
	}
	return clone
}

// Clone returns a deep copy of FunctionCall.
func (fc *FunctionCall) Clone() *FunctionCall {
	if fc == nil {
		return nil
	}
	clone := &FunctionCall{
		Name: fc.Name,
	}
	if fc.Args != nil {
		clone.Args = make(map[string]interface{}, len(fc.Args))
		for k, v := range fc.Args {
			clone.Args[k] = cloneValue(v)
		}
	}
	return clone
}

// Clone returns a deep copy of FunctionResponse.
func (fr *FunctionResponse) Clone() *FunctionResponse {
	if fr == nil {
		return nil
	}
	clone := &FunctionResponse{
		Name: fr.Name,
	}
	if fr.Response != nil {
		clone.Response = make(map[string]interface{}, len(fr.Response))
		for k, v := range fr.Response {
			clone.Response[k] = cloneValue(v)
		}
	}
	return clone
}

func cloneValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		newMap := make(map[string]interface{}, len(val))
		for mk, mv := range val {
			newMap[mk] = cloneValue(mv)
		}
		return newMap
	case []interface{}:
		newSlice := make([]interface{}, len(val))
		for i, sv := range val {
			newSlice[i] = cloneValue(sv)
		}
		return newSlice
	default:
		// For primitive types (string, float64, bool, nil) which are common in JSON,
		// shallow copy is sufficient as they are immutable in Go.
		return v
	}
}

// Equal returns true if two Content objects are logically equivalent.
func (c *Content) Equal(other *Content) bool {
	if c == nil || other == nil {
		return c == other
	}
	if c.Role != other.Role || len(c.Parts) != len(other.Parts) {
		return false
	}
	for i := range c.Parts {
		if !c.Parts[i].Equal(other.Parts[i]) {
			return false
		}
	}
	return true
}

// Equal returns true if two Part objects are logically equivalent.
func (p *Part) Equal(other *Part) bool {
	if p == nil || other == nil {
		return p == other
	}
	if p.Text != other.Text || p.Thought != other.Thought || p.AssetID != other.AssetID {
		return false
	}
	if !bytes.Equal(p.ThoughtSignature, other.ThoughtSignature) {
		return false
	}
	if (p.InlineData == nil) != (other.InlineData == nil) {
		return false
	}
	if p.InlineData != nil {
		if p.InlineData.MIMEType != other.InlineData.MIMEType || !bytes.Equal(p.InlineData.Data, other.InlineData.Data) {
			return false
		}
	}
	if !reflect.DeepEqual(p.FunctionCall, other.FunctionCall) {
		return false
	}
	if !reflect.DeepEqual(p.FunctionResponse, other.FunctionResponse) {
		return false
	}
	return true
}
