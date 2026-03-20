// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	IsThought        bool              `json:"is_thought,omitempty"`
	ThoughtSignature []byte            `json:"thought_signature,omitempty"`
	AssetID          string            `json:"asset_id,omitempty"` // Local reference for persistence
}

type Blob struct {
	MIMEType string `json:"mime_type,omitempty"`
	Data     []byte `json:"data,omitempty"`
}

type FunctionCall struct {
	ID   string                 `json:"id,omitempty"`
	Name string                 `json:"name,omitempty"`
	Args map[string]interface{} `json:"args,omitempty"`
}

type FunctionResponse struct {
	ID       string                 `json:"id,omitempty"`
	Name     string                 `json:"name,omitempty"`
	Response map[string]interface{} `json:"response,omitempty"`
}

// Metrics represents the token usage and timing for a single API turn.
type Metrics struct {
	Timestamp              string  `json:"timestamp"`
	Provider               string  `json:"provider,omitempty"`
	Model                  string  `json:"model,omitempty"`
	CachedTokens           int32   `json:"cached_tokens"`
	PromptTokens           int32   `json:"prompt_tokens"`
	ResponseTokens         int32   `json:"response_tokens"`
	TotalTokens            int32   `json:"total_tokens"`
	ThinkingTokens         int32   `json:"thinking_tokens,omitempty"`
	SearchQueries          int     `json:"search_queries,omitempty"`
	Duration               float64 `json:"duration"`
	ToolDuration           float64 `json:"tool_duration,omitempty"`
	CumulativeToolDuration float64 `json:"cumulative_tool_duration,omitempty"`
	Cost                   float64 `json:"cost,omitempty"` // USD cost for this turn or summary
	IsSummary              bool    `json:"is_summary,omitempty"`
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

// ExtendedClient defines a client that provides both raw LLM operations and resilient Gateway capabilities.
type ExtendedClient interface {
	LLMClient
	LLMGateway
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
	if len(c.Parts) > 0 {
		last := c.Parts[len(c.Parts)-1]
		if last.canMergeWith(p) {
			last.Text += p.Text
			if len(last.ThoughtSignature) == 0 && len(p.ThoughtSignature) > 0 {
				last.ThoughtSignature = p.ThoughtSignature
			}
			return
		}
	}

	// Otherwise append new part.
	c.Parts = append(c.Parts, p.clone())
}

func (p *Part) isPure() bool {
	return p.FunctionCall == nil && p.FunctionResponse == nil && p.InlineData == nil
}

func (p *Part) canMergeWith(other *Part) bool {
	if !p.isPure() || !other.isPure() {
		return false
	}
	// Both must have the same IsThought value
	return p.IsThought == other.IsThought
}

// clone returns a deep copy of Content.
func (c *Content) clone() *Content {
	clone := &Content{
		Role:       c.Role,
		TokenCount: c.TokenCount,
		Pinned:     c.Pinned,
	}
	if c.Parts != nil {
		clone.Parts = make([]*Part, len(c.Parts))
		for i, p := range c.Parts {
			clone.Parts[i] = p.clone()
		}
	}
	if c.TransientParts != nil {
		clone.TransientParts = make([]*Part, len(c.TransientParts))
		for i, p := range c.TransientParts {
			clone.TransientParts[i] = p.clone()
		}
	}
	return clone
}

// clone returns a deep copy of Part.
func (p *Part) clone() *Part {
	if p == nil {
		return nil
	}
	clone := &Part{
		Text:             p.Text,
		IsThought:        p.IsThought,
		AssetID:          p.AssetID,
		InlineData:       p.InlineData.clone(),
		FunctionCall:     p.FunctionCall.clone(),
		FunctionResponse: p.FunctionResponse.clone(),
	}
	clone.ThoughtSignature = bytes.Clone(p.ThoughtSignature)
	return clone
}

// clone returns a deep copy of Blob.
func (b *Blob) clone() *Blob {
	if b == nil {
		return nil
	}
	clone := &Blob{
		MIMEType: b.MIMEType,
	}
	clone.Data = bytes.Clone(b.Data)
	return clone
}

// clone returns a deep copy of FunctionCall.
func (fc *FunctionCall) clone() *FunctionCall {
	if fc == nil {
		return nil
	}
	clone := &FunctionCall{
		ID:   fc.ID,
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

// clone returns a deep copy of FunctionResponse.
func (fr *FunctionResponse) clone() *FunctionResponse {
	if fr == nil {
		return nil
	}
	clone := &FunctionResponse{
		ID:   fr.ID,
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

// CloneContent returns a deep copy of Content.
func CloneContent(c *Content) *Content {
	if c == nil {
		return nil
	}
	return c.clone()
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

// equal returns true if two Content objects are logically equivalent.
func (c *Content) equal(other *Content) bool {
	if c.Role != other.Role || len(c.Parts) != len(other.Parts) {
		return false
	}
	for i := range c.Parts {
		if !c.Parts[i].equal(other.Parts[i]) {
			return false
		}
	}
	return true
}

// equal returns true if two Blob objects are equivalent.
func (b *Blob) equal(other *Blob) bool {
	if b == nil || other == nil {
		return b == other
	}
	return b.MIMEType == other.MIMEType && bytes.Equal(b.Data, other.Data)
}

// equal returns true if two FunctionCall objects are equivalent.
func (fc *FunctionCall) equal(other *FunctionCall) bool {
	if fc == nil || other == nil {
		return fc == other
	}
	if fc.ID != other.ID || fc.Name != other.Name {
		return false
	}
	return reflect.DeepEqual(fc.Args, other.Args)
}

// equal returns true if two FunctionResponse objects are equivalent.
func (fr *FunctionResponse) equal(other *FunctionResponse) bool {
	if fr == nil || other == nil {
		return fr == other
	}
	if fr.ID != other.ID || fr.Name != other.Name {
		return false
	}
	return reflect.DeepEqual(fr.Response, other.Response)
}

// equal returns true if two Part objects are logically equivalent.
func (p *Part) equal(other *Part) bool {
	if p.Text != other.Text || p.IsThought != other.IsThought || p.AssetID != other.AssetID {
		return false
	}
	if !bytes.Equal(p.ThoughtSignature, other.ThoughtSignature) {
		return false
	}
	if !p.InlineData.equal(other.InlineData) {
		return false
	}
	if !p.FunctionCall.equal(other.FunctionCall) {
		return false
	}
	return p.FunctionResponse.equal(other.FunctionResponse)
}

// EqualContent returns true if two Content objects are logically equivalent.
func EqualContent(a, b *Content) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.equal(b)
}

// IsEmpty returns true if the part contains no meaningful content for the LLM.
func (p *Part) IsEmpty() bool {
	return p.Text == "" &&
		p.InlineData == nil &&
		p.FunctionCall == nil &&
		p.FunctionResponse == nil &&
		p.AssetID == "" &&
		!p.IsThought &&
		len(p.ThoughtSignature) == 0
}

// Validate ensures the Content object is in a valid state for the domain.
// It removes any nil pointers from the Parts and TransientParts slices.
func (c *Content) Validate() {
	if c == nil {
		return
	}
	if len(c.Parts) > 0 {
		cleanParts := make([]*Part, 0, len(c.Parts))
		for _, p := range c.Parts {
			if p != nil {
				cleanParts = append(cleanParts, p)
			}
		}
		c.Parts = cleanParts
	}
	if len(c.TransientParts) > 0 {
		cleanTransientParts := make([]*Part, 0, len(c.TransientParts))
		for _, p := range c.TransientParts {
			if p != nil {
				cleanTransientParts = append(cleanTransientParts, p)
			}
		}
		c.TransientParts = cleanTransientParts
	}
}

// ValidateStructure checks the content for structural integrity (e.g., no nil parts).
// It returns an error if the content is malformed.
func (c *Content) ValidateStructure() error {
	if c == nil {
		return errors.New("nil content")
	}
	for i, p := range c.Parts {
		if p == nil {
			return fmt.Errorf("nil part at index %d", i)
		}
	}
	return nil
}
