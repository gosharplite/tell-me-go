// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package api

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"google.golang.org/genai"
)

// ToSDKContent converts internal Content to genai.Content.
func ToSDKContent(ctx context.Context, c *llm.Content, resolver llm.AssetResolver) *genai.Content {
	if c == nil {
		return nil
	}
	res := &genai.Content{
		Role: c.Role,
	}
	for _, p := range c.Parts {
		res.Parts = append(res.Parts, ToSDKPart(ctx, p, resolver))
	}
	return res
}

// ToSDKPart converts internal Part to genai.Part.
func ToSDKPart(ctx context.Context, p *llm.Part, resolver llm.AssetResolver) *genai.Part {
	if p == nil {
		return nil
	}
	res := &genai.Part{
		Text:             p.Text,
		Thought:          p.Thought,
		ThoughtSignature: p.ThoughtSignature,
	}

	if p.InlineData != nil {
		res.InlineData = &genai.Blob{
			MIMEType: p.InlineData.MIMEType,
			Data:     p.InlineData.Data,
		}
	}

	if p.FunctionCall != nil {
		res.FunctionCall = &genai.FunctionCall{
			Name: p.FunctionCall.Name,
			Args: p.FunctionCall.Args,
		}
	}

	if p.FunctionResponse != nil {
		res.FunctionResponse = &genai.FunctionResponse{
			Name:     p.FunctionResponse.Name,
			Response: p.FunctionResponse.Response,
		}
	}

	// Lazy hydration: if asset ID is present and data is missing, resolve it.
	if p.AssetID != "" && resolver != nil && (res.InlineData == nil || len(res.InlineData.Data) == 0) {
		data, err := resolver.Resolve(ctx, p.AssetID)
		if err == nil {
			if res.InlineData == nil {
				res.InlineData = &genai.Blob{}
			} else {
				// Shallow copy the blob to avoid mutating the original in the internal Part
				blobClone := *res.InlineData
				res.InlineData = &blobClone
			}
			res.InlineData.Data = data
		}
	}

	return res
}

// FromSDKContent converts genai.Content to internal Content.
func FromSDKContent(c *genai.Content) *llm.Content {
	if c == nil {
		return nil
	}
	res := &llm.Content{
		Role: c.Role,
	}
	for _, p := range c.Parts {
		res.Parts = append(res.Parts, FromSDKPart(p))
	}
	return res
}

// FromSDKPart converts genai.Part to internal Part.
func FromSDKPart(p *genai.Part) *llm.Part {
	if p == nil {
		return nil
	}
	res := &llm.Part{
		Text:             p.Text,
		Thought:          p.Thought,
		ThoughtSignature: p.ThoughtSignature,
	}

	if p.InlineData != nil {
		res.InlineData = &llm.Blob{
			MIMEType: p.InlineData.MIMEType,
			Data:     p.InlineData.Data,
		}
	}

	if p.FunctionCall != nil {
		res.FunctionCall = &llm.FunctionCall{
			Name: p.FunctionCall.Name,
			Args: p.FunctionCall.Args,
		}
	}

	if p.FunctionResponse != nil {
		res.FunctionResponse = &llm.FunctionResponse{
			Name:     p.FunctionResponse.Name,
			Response: p.FunctionResponse.Response,
		}
	}

	return res
}
