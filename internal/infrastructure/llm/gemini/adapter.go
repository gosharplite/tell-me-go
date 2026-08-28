// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package gemini

import (
	"context"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"google.golang.org/genai"
)

// toSDKContent converts internal Content to genai.Content.
func toSDKContent(ctx context.Context, c *llm.Content, resolver llm.AssetResolver) *genai.Content {
	if c == nil {
		return nil
	}
	res := &genai.Content{
		Role:  c.Role,
		Parts: make([]*genai.Part, 0, len(c.Parts)+len(c.TransientParts)),
	}
	for _, p := range c.Parts {
		res.Parts = append(res.Parts, toSDKPart(ctx, p, resolver))
	}
	for _, p := range c.TransientParts {
		res.Parts = append(res.Parts, toSDKPart(ctx, p, resolver))
	}
	// Normalize at the wire boundary, after conversion and hydration, so the
	// partition runs on the final wire shape (hydrateAsset has populated
	// InlineData for every blob by this point). TransientParts exemption:
	// the sole production writer of TransientParts is warning_injector.go:109
	// (injectWarning), which appends a Text-only part — no partition class
	// can ever arrive via the transient suffix, so the three-zone partition
	// is only ever exercised by c.Parts-derived content.
	res.Parts = normalizeUserTurnParts(res.Parts, c.Role)
	return res
}

// normalizeUserTurnParts reorders a user-role turn's parts into the
// [InlineData][FunctionResponse][other] zones the Gemini/Vertex AI parser
// requires. The parser invalidates a user turn whose FunctionResponse part
// precedes an InlineData part (see #1441): the preceding model turn
// (containing the FunctionCall) appears unanswered, which triggers
// "Error 400: Requests ending with a model turn are not supported" on the
// next inference cycle. This normalization heals persisted/reloaded
// history — the assembled user turn is written to history.jsonl and re-sent
// verbatim on later turns — not just freshly assembled turns (the executor
// already guarantees the order via two-pass assembly, #1442/#1444).
//
// The gate is load-bearing: only user turns mixing InlineData with
// FunctionResponse are reordered; text-only, blob-only, and text+blob turns
// (and every model/system role) are returned unchanged. The partition is
// stable (relative order preserved within each zone) and idempotent
// (already-canonical input is a no-op). Nil parts are classified as "other"
// so the slice length is invariant.
func normalizeUserTurnParts(parts []*genai.Part, role string) []*genai.Part {
	if role != "user" {
		return parts
	}

	var inline, responses, other []*genai.Part
	for _, p := range parts {
		if p != nil && p.InlineData != nil {
			inline = append(inline, p)
		} else if p != nil && p.FunctionResponse != nil {
			responses = append(responses, p)
		} else {
			other = append(other, p)
		}
	}
	if len(inline) == 0 || len(responses) == 0 {
		return parts
	}
	return append(append(inline, responses...), other...)
}

// toSDKPart converts internal Part to genai.Part.
func toSDKPart(ctx context.Context, p *llm.Part, resolver llm.AssetResolver) *genai.Part {
	if p == nil {
		return nil
	}

	res := &genai.Part{
		Text:             p.Text,
		Thought:          p.IsThought,
		ThoughtSignature: p.ThoughtSignature,
		InlineData:       toSDKBlob(p.InlineData),
		FunctionCall:     toSDKFunctionCall(p.FunctionCall),
		FunctionResponse: toSDKFunctionResponse(p.FunctionResponse),
	}

	hydrateAsset(ctx, p, res, resolver)
	return res
}

func toSDKBlob(b *llm.Blob) *genai.Blob {
	if b == nil {
		return nil
	}
	return &genai.Blob{
		MIMEType: b.MIMEType,
		Data:     b.Data,
	}
}

func toSDKFunctionCall(f *llm.FunctionCall) *genai.FunctionCall {
	if f == nil {
		return nil
	}
	return &genai.FunctionCall{
		ID:   f.ID,
		Name: f.Name,
		Args: f.Args,
	}
}

func toSDKFunctionResponse(f *llm.FunctionResponse) *genai.FunctionResponse {
	if f == nil {
		return nil
	}
	return &genai.FunctionResponse{
		ID:       f.ID,
		Name:     f.Name,
		Response: f.Response,
	}
}

func hydrateAsset(ctx context.Context, p *llm.Part, res *genai.Part, resolver llm.AssetResolver) {
	if p.AssetID == "" || resolver == nil {
		return
	}
	// Skip if data is already present
	if res.InlineData != nil && len(res.InlineData.Data) > 0 {
		return
	}

	data, err := resolver.Resolve(ctx, p.AssetID)
	if err != nil {
		return
	}

	if res.InlineData == nil {
		res.InlineData = &genai.Blob{}
	} else {
		// Shallow copy the blob to avoid mutating the original in the internal Part
		blobClone := *res.InlineData
		res.InlineData = &blobClone
	}
	res.InlineData.Data = data
}

// fromSDKContent converts genai.Content to internal Content.
func fromSDKContent(c *genai.Content) *llm.Content {
	if c == nil {
		return nil
	}
	res := &llm.Content{
		Role:  c.Role,
		Parts: make([]*llm.Part, 0, len(c.Parts)),
	}
	for i, p := range c.Parts {
		if p == nil {
			continue
		}
		res.Parts = append(res.Parts, fromSDKPart(p, i))
	}
	res.Validate() // Final boundary sanitization
	return res
}

// fromSDKPart converts genai.Part to internal Part.
func fromSDKPart(p *genai.Part, index int) *llm.Part {
	if p == nil {
		return nil
	}

	return &llm.Part{
		Text:             p.Text,
		IsThought:        p.Thought,
		ThoughtSignature: p.ThoughtSignature,
		InlineData:       fromSDKBlob(p.InlineData),
		FunctionCall:     fromSDKFunctionCall(p.FunctionCall, index),
		FunctionResponse: fromSDKFunctionResponse(p.FunctionResponse, index),
	}
}

func fromSDKBlob(b *genai.Blob) *llm.Blob {
	if b == nil {
		return nil
	}
	return &llm.Blob{
		MIMEType: b.MIMEType,
		Data:     b.Data,
	}
}

func fromSDKFunctionCall(f *genai.FunctionCall, index int) *llm.FunctionCall {
	if f == nil {
		return nil
	}
	// Prefer the SDK's internal ID if available (supported in newer Gemini versions).
	id := f.ID
	if id == "" {
		// Fallback to a deterministic ID to satisfy orchestration requirements.
		id = fmt.Sprintf("gemini-call-%d-%s", index, f.Name)
	}
	return &llm.FunctionCall{
		ID:   id,
		Name: f.Name,
		Args: f.Args,
	}
}

func fromSDKFunctionResponse(f *genai.FunctionResponse, index int) *llm.FunctionResponse {
	if f == nil {
		return nil
	}
	// Prefer the SDK's internal ID if available.
	id := f.ID
	if id == "" {
		// Deterministic ID matching the one in fromSDKFunctionCall
		id = fmt.Sprintf("gemini-call-%d-%s", index, f.Name)
	}
	return &llm.FunctionResponse{
		ID:       id,
		Name:     f.Name,
		Response: f.Response,
	}
}
