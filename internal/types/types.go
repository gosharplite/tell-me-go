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

// AssetResolver defines the interface for lazy hydration of binary assets.
type AssetResolver interface {
	Resolve(assetID string) ([]byte, error)
}

// ToolDeclaration represents a function that can be called by the model.
type ToolDeclaration struct {
	Name        string
	Description string
	Parameters  *Schema
}

// Schema represents the parameters of a tool.
type Schema struct {
	Type        string
	Description string
	Properties  map[string]*Schema
	Required    []string
	Enum        []string
	Items       *Schema
}

// ToSDK converts internal Content to genai.Content.
func (c *Content) ToSDK(resolver AssetResolver) *genai.Content {
	if c == nil {
		return nil
	}
	res := &genai.Content{
		Role: c.Role,
	}
	for _, p := range c.Parts {
		res.Parts = append(res.Parts, p.ToSDK(resolver))
	}
	return res
}

// ToSDK converts internal Part to genai.Part.
func (p *Part) ToSDK(resolver AssetResolver) *genai.Part {
	if p == nil {
		return nil
	}
	res := &genai.Part{
		Text:    p.Text,
		Thought: p.Thought,
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
		data, err := resolver.Resolve(p.AssetID)
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
	res := &Part{
		Text:             p.Text,
		Thought:          p.Thought,
		ThoughtSignature: p.ThoughtSignature,
	}

	if p.InlineData != nil {
		res.InlineData = &Blob{
			MIMEType: p.InlineData.MIMEType,
			Data:     p.InlineData.Data,
		}
	}

	if p.FunctionCall != nil {
		res.FunctionCall = &FunctionCall{
			Name: p.FunctionCall.Name,
			Args: p.FunctionCall.Args,
		}
	}

	if p.FunctionResponse != nil {
		res.FunctionResponse = &FunctionResponse{
			Name:     p.FunctionResponse.Name,
			Response: p.FunctionResponse.Response,
		}
	}

	return res
}

// LLMClient defines the interface for AI model providers.
type LLMClient interface {
	SendChat(ctx context.Context, history []*Content, tools []*ToolDeclaration, resolver AssetResolver) (*Content, *Metrics, error)
	StreamChat(ctx context.Context, history []*Content, tools []*ToolDeclaration, resolver AssetResolver, callback func(*Content)) (*Metrics, error)
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
