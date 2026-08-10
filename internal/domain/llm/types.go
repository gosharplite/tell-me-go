// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/google/uuid"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// Shared API models for LLM interactions.

type Content struct {
	Role           string  `json:"role"`
	ID             string  `json:"id,omitempty"`
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
	CacheWriteTokens       int32   `json:"cache_write_tokens,omitempty"`
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
	TrafficType            string  `json:"traffic_type,omitempty"`
}

// AssetResolver defines the interface for lazy hydration of binary assets.
type AssetResolver interface {
	Resolve(ctx context.Context, assetID string) ([]byte, error)
}

// LLMClient defines the interface for AI model providers.
type LLMClient interface {
	SendChat(ctx context.Context, history []*Content, tools []*tools.ToolDeclaration, resolver AssetResolver) (*Content, *Metrics, error)
	GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error)
	// ExtractDocument extracts text content from a document (PDF, DOCX, MD, etc.)
	// by uploading it to the provider's file extraction API. Returns the extracted
	// text. Providers that don't support document extraction return ErrNotImplemented.
	ExtractDocument(ctx context.Context, data []byte, filename string) (string, error)
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

	// ErrNotImplemented is returned by provider methods that are not supported
	// by a particular provider (e.g., GenerateImages on OpenAI, ExtractDocument
	// on Anthropic/Gemini).
	ErrNotImplemented = errors.New("not implemented for this provider")

	// errBudgetExceeded is returned when the session cost exceeds the configured budget.
	errBudgetExceeded = errors.New("session budget exceeded")
)

// addPart merges a new part into the content, appending or joining text parts as appropriate.
func (c *Content) addPart(p *Part) {
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
		ID:         c.ID,
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

// NewID returns a new unique identifier string (UUID v4).
// Architect-acceptance (2026-07): delegation to uuid.New().String() — same
// acceptance class as delegation wrappers like mockFileSystem.Chmod and
// domainFS.Chmod. See: docs/architect/INTENTIONAL_NON_FIXES.md.
func NewID() string {
	return uuid.New().String()
}

// CloneContent returns a deep copy of Content.
func CloneContent(c *Content) *Content {
	if c == nil {
		return nil
	}
	return c.clone()
}

// CloneArena provides arena-backed allocation for deep-cloning Content
// graphs. All Content/Part/Blob/FunctionCall/FunctionResponse structs
// produced by a CloneContentSlice call are laid out in contiguous backing
// arrays owned by the arena, reducing the per-window allocation count from
// ~3 per entry to a small constant (one backing per struct type). See
// docs/architect/1321-f1-baseline.md §5 for the measurement rationale.
//
// SEMANTICS: output is fully-owned deep copies — nothing reachable from a
// clone shares mutable state with the source. Immutable string headers are
// shared exactly as CloneContent shares them; []byte (ThoughtSignature,
// Blob.Data) is bytes.Clone'd. Each entry's Parts/TransientParts slice is
// a sub-slice of the arena's pointer backing created with a full slice
// expression (cap == len), so appending to any clone's Parts can never
// clobber a neighbour's slots — it reallocates, exactly as a standalone
// slice would.
//
// LIFETIME: single-use. The arena must NOT be reused across rounds while a
// prior clone may still be referenced (Prepare's output outlives Prepare —
// it is stored in TurnState). A fresh arena per GetWindow call is the
// compliant usage: the backing arrays become garbage with the returned
// window. No pooling, no global state, no synchronization.
type CloneArena struct {
	contents  []Content
	partsPtrs []*Part
	parts     []Part
	blobs     []Blob
	calls     []FunctionCall
	responses []FunctionResponse
}

// NewCloneArena creates a fresh arena. capacity is a hint for the number
// of Content entries expected; it pre-sizes the contents backing so that
// CloneContentSlice can reuse it when the window fits.
func NewCloneArena(capacity int) *CloneArena {
	if capacity < 0 {
		capacity = 0
	}
	return &CloneArena{
		contents: make([]Content, 0, capacity),
	}
}

// CloneContent deep-copies a single Content into the arena. It returns nil
// for a nil input. The result is a fully-owned deep copy (see CloneArena).
func (a *CloneArena) CloneContent(c *Content) *Content {
	if c == nil {
		return nil
	}
	return a.cloneContent(c)
}

// CloneContentSlice deep-copies a slice of Content into the arena,
// preserving length and order. Nil entries map to nil. The result is a
// fully-owned deep copy with no structural sharing with the source (see
// CloneArena). The arena's backings are sized exactly by a counting pass,
// so no reallocation occurs during fill and every pointer stays stable.
func (a *CloneArena) CloneContentSlice(src []*Content) []*Content {
	out := make([]*Content, len(src))
	if len(src) == 0 {
		return out
	}

	// Counting pass: exact sizes for every arena backing (single allocation
	// each, no growth during fill).
	nNonNil := 0
	var nParts, nTransParts, nBlobs, nCalls, nResponses int
	countParts := func(parts []*Part) {
		for _, p := range parts {
			if p == nil {
				continue
			}
			if p.InlineData != nil {
				nBlobs++
			}
			if p.FunctionCall != nil {
				nCalls++
			}
			if p.FunctionResponse != nil {
				nResponses++
			}
		}
	}
	for _, c := range src {
		if c == nil {
			continue
		}
		nNonNil++
		nParts += len(c.Parts)
		nTransParts += len(c.TransientParts)
		countParts(c.Parts)
		countParts(c.TransientParts)
	}

	if cap(a.contents) < nNonNil {
		a.contents = make([]Content, 0, nNonNil)
	}
	// Parts and TransientParts share the same struct/pointer backings
	// (both are []*Part), so size them for the combined count. The
	// pointer backing is allocated whenever any content exists — even at
	// zero capacity (a free zerobase allocation) — so that empty-but-non-nil
	// Parts/TransientParts in the source clone to empty-but-non-nil slices,
	// exactly like CloneContent's make([]*Part, 0).
	if nNonNil > 0 {
		a.partsPtrs = make([]*Part, 0, nParts+nTransParts)
	}
	if nParts+nTransParts > 0 {
		a.parts = make([]Part, 0, nParts+nTransParts)
	}
	if nBlobs > 0 {
		a.blobs = make([]Blob, 0, nBlobs)
	}
	if nCalls > 0 {
		a.calls = make([]FunctionCall, 0, nCalls)
	}
	if nResponses > 0 {
		a.responses = make([]FunctionResponse, 0, nResponses)
	}

	for i, c := range src {
		if c == nil {
			out[i] = nil
			continue
		}
		out[i] = a.cloneContent(c)
	}
	return out
}

func (a *CloneArena) cloneContent(c *Content) *Content {
	a.contents = append(a.contents, Content{})
	out := &a.contents[len(a.contents)-1]
	out.Role = c.Role
	out.ID = c.ID
	out.TokenCount = c.TokenCount
	out.Pinned = c.Pinned
	if c.Parts != nil {
		out.Parts = a.cloneParts(c.Parts)
	}
	if c.TransientParts != nil {
		out.TransientParts = a.cloneParts(c.TransientParts)
	}
	return out
}

// cloneParts clones src into the shared pointer/struct backings and returns
// a sub-slice with cap == len (full slice expression), so the returned slice
// behaves exactly like a standalone allocation under append. The pointer
// backing is created on first use (exact-cap for this call), so the
// single-entry path keeps empty-but-non-nil Parts non-nil; CloneContentSlice
// pre-sizes the backing exactly and this guard becomes a no-op.
func (a *CloneArena) cloneParts(src []*Part) []*Part {
	if a.partsPtrs == nil {
		a.partsPtrs = make([]*Part, 0, len(src))
	}
	start := len(a.partsPtrs)
	for _, p := range src {
		if p == nil {
			a.partsPtrs = append(a.partsPtrs, nil)
			continue
		}
		a.parts = append(a.parts, Part{})
		cp := &a.parts[len(a.parts)-1]
		cp.Text = p.Text
		cp.IsThought = p.IsThought
		cp.AssetID = p.AssetID
		cp.ThoughtSignature = bytes.Clone(p.ThoughtSignature)
		if p.InlineData != nil {
			cp.InlineData = a.cloneBlob(p.InlineData)
		}
		if p.FunctionCall != nil {
			cp.FunctionCall = a.cloneCall(p.FunctionCall)
		}
		if p.FunctionResponse != nil {
			cp.FunctionResponse = a.cloneResponse(p.FunctionResponse)
		}
		a.partsPtrs = append(a.partsPtrs, cp)
	}
	return a.partsPtrs[start:len(a.partsPtrs):len(a.partsPtrs)]
}

func (a *CloneArena) cloneBlob(b *Blob) *Blob {
	a.blobs = append(a.blobs, Blob{})
	out := &a.blobs[len(a.blobs)-1]
	out.MIMEType = b.MIMEType
	out.Data = bytes.Clone(b.Data)
	return out
}

func (a *CloneArena) cloneCall(fc *FunctionCall) *FunctionCall {
	a.calls = append(a.calls, FunctionCall{})
	out := &a.calls[len(a.calls)-1]
	out.ID = fc.ID
	out.Name = fc.Name
	if fc.Args != nil {
		out.Args = make(map[string]interface{}, len(fc.Args))
		for k, v := range fc.Args {
			out.Args[k] = cloneValue(v)
		}
	}
	return out
}

func (a *CloneArena) cloneResponse(fr *FunctionResponse) *FunctionResponse {
	a.responses = append(a.responses, FunctionResponse{})
	out := &a.responses[len(a.responses)-1]
	out.ID = fr.ID
	out.Name = fr.Name
	if fr.Response != nil {
		out.Response = make(map[string]interface{}, len(fr.Response))
		for k, v := range fr.Response {
			out.Response[k] = cloneValue(v)
		}
	}
	return out
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
	for i, p := range c.TransientParts {
		if p == nil {
			return fmt.Errorf("nil transient part at index %d", i)
		}
	}
	return nil
}
