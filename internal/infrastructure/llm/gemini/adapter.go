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
	return res
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
		res.Parts = append(res.Parts, fromSDKPart(p, i))
	}
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
