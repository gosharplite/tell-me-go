// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package gemini

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"google.golang.org/genai"
)

type mockResolver struct {
	resolveFunc func(ctx context.Context, assetID string) ([]byte, error)
}

func (m *mockResolver) Resolve(ctx context.Context, assetID string) ([]byte, error) {
	return m.resolveFunc(ctx, assetID)
}

func TestPart_Conversion(t *testing.T) {
	signature := []byte("test-signature")
	sdkPart := &genai.Part{
		Text:             "hello",
		Thought:          true,
		ThoughtSignature: signature,
		InlineData: &genai.Blob{
			MIMEType: "image/jpeg",
			Data:     []byte("fake-jpeg"),
		},
	}

	internalPart := fromSDKPart(sdkPart, 0)

	if internalPart.Text != sdkPart.Text {
		t.Errorf("expected text %s, got %s", sdkPart.Text, internalPart.Text)
	}
	if !internalPart.IsThought {
		t.Errorf("expected IsThought true, got %v", internalPart.IsThought)
	}
	if !reflect.DeepEqual(internalPart.ThoughtSignature, sdkPart.ThoughtSignature) {
		t.Errorf("expected signature %v, got %v", sdkPart.ThoughtSignature, internalPart.ThoughtSignature)
	}
	if internalPart.InlineData == nil {
		t.Fatal("expected InlineData to be populated")
	}
	if internalPart.InlineData.MIMEType != "image/jpeg" {
		t.Errorf("expected image/jpeg, got %s", internalPart.InlineData.MIMEType)
	}
	if !reflect.DeepEqual(internalPart.InlineData.Data, sdkPart.InlineData.Data) {
		t.Errorf("expected data %v, got %v", sdkPart.InlineData.Data, internalPart.InlineData.Data)
	}

	backToSDK := toSDKPart(context.Background(), internalPart, nil)
	if !reflect.DeepEqual(backToSDK, sdkPart) {
		t.Errorf("roundtrip failed: expected %+v, got %+v", sdkPart, backToSDK)
	}
}

func TestPart_ToSDK_LazyHydration(t *testing.T) {
	assetID := "test-asset"
	assetData := []byte("image-data")
	p := &llm.Part{
		AssetID: assetID,
		InlineData: &llm.Blob{
			MIMEType: "image/png",
		},
	}

	resolver := &mockResolver{
		resolveFunc: func(ctx context.Context, id string) ([]byte, error) {
			if id == assetID {
				return assetData, nil
			}
			return nil, nil
		},
	}

	sdkPart := toSDKPart(context.Background(), p, resolver)

	if sdkPart.InlineData == nil {
		t.Fatal("expected InlineData to be populated")
	}
	if !reflect.DeepEqual(sdkPart.InlineData.Data, assetData) {
		t.Errorf("expected data %v, got %v", assetData, sdkPart.InlineData.Data)
	}
	if sdkPart.InlineData.MIMEType != "image/png" {
		t.Errorf("expected MIMEType image/png, got %s", sdkPart.InlineData.MIMEType)
	}

	// Verify original Part was NOT mutated
	if len(p.InlineData.Data) != 0 {
		t.Error("original Part should not have been mutated")
	}
}

func TestContent_ToSDK(t *testing.T) {
	content := &llm.Content{
		Role: "model",
		Parts: []*llm.Part{
			{
				Text:             "thinking",
				IsThought:        true,
				ThoughtSignature: []byte("sig"),
			},
		},
	}

	sdkContent := toSDKContent(context.Background(), content, nil)
	if sdkContent.Role != content.Role {
		t.Errorf("expected role %s, got %s", content.Role, sdkContent.Role)
	}
	if len(sdkContent.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(sdkContent.Parts))
	}
	if !reflect.DeepEqual(sdkContent.Parts[0].ThoughtSignature, content.Parts[0].ThoughtSignature) {
		t.Errorf("expected signature %v, got %v", content.Parts[0].ThoughtSignature, sdkContent.Parts[0].ThoughtSignature)
	}
}

func TestPart_FunctionConversion(t *testing.T) {
	sdkPart := &genai.Part{
		FunctionCall: &genai.FunctionCall{
			Name: "test_tool",
			Args: map[string]interface{}{"arg1": "val1"},
		},
	}

	internalPart := fromSDKPart(sdkPart, 0)
	if internalPart.FunctionCall.Name != "test_tool" {
		t.Errorf("expected test_tool, got %s", internalPart.FunctionCall.Name)
	}

	backToSDK := toSDKPart(context.Background(), internalPart, nil)
	if backToSDK.FunctionCall.Name != "test_tool" {
		t.Errorf("roundtrip failed for function call")
	}

	sdkPartResp := &genai.Part{
		FunctionResponse: &genai.FunctionResponse{
			Name:     "test_tool",
			Response: map[string]interface{}{"result": "ok"},
		},
	}
	internalPartResp := fromSDKPart(sdkPartResp, 0)
	if internalPartResp.FunctionResponse.Name != "test_tool" {
		t.Errorf("expected test_tool, got %s", internalPartResp.FunctionResponse.Name)
	}

	backToSDKResp := toSDKPart(context.Background(), internalPartResp, nil)
	if backToSDKResp.FunctionResponse.Name != "test_tool" {
		t.Errorf("roundtrip failed for function response")
	}
}

func TestContent_Conversion_Nil(t *testing.T) {
	if toSDKContent(context.Background(), nil, nil) != nil {
		t.Error("ToSDKContent(nil) should be nil")
	}
	if fromSDKContent(nil) != nil {
		t.Error("FromSDKContent(nil) should be nil")
	}
	if toSDKPart(context.Background(), nil, nil) != nil {
		t.Error("ToSDKPart(nil) should be nil")
	}
	if fromSDKPart(nil, 0) != nil {
		t.Error("FromSDKPart(nil) should be nil")
	}
}

func TestFromSDKContent_NilPartFiltering(t *testing.T) {
	// Gap 10: fromSDKContent must skip nil entries in the Parts slice.
	// The Gemini SDK can return nil entries after safety filtering of
	// individual parts within a content block.
	sdkContent := &genai.Content{
		Role: "model",
		Parts: []*genai.Part{
			nil, // should be filtered
			{Text: "valid-part-1"},
			nil, // should be filtered
			{Text: "valid-part-2"},
		},
	}

	result := fromSDKContent(sdkContent)

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Parts) != 2 {
		t.Fatalf("expected 2 parts (nil filtered), got %d", len(result.Parts))
	}
	if result.Parts[0].Text != "valid-part-1" {
		t.Errorf("parts[0] text: got %q, want %q", result.Parts[0].Text, "valid-part-1")
	}
	if result.Parts[1].Text != "valid-part-2" {
		t.Errorf("parts[1] text: got %q, want %q", result.Parts[1].Text, "valid-part-2")
	}
}

func TestContent_TransientParts(t *testing.T) {
	content := &llm.Content{
		Role: "user",
		Parts: []*llm.Part{
			{Text: "part1"},
		},
		TransientParts: []*llm.Part{
			{Text: "transient"},
		},
	}

	sdkContent := toSDKContent(context.Background(), content, nil)
	if len(sdkContent.Parts) != 2 {
		t.Fatalf("expected 2 parts (1 regular + 1 transient), got %d", len(sdkContent.Parts))
	}
	if sdkContent.Parts[1].Text != "transient" {
		t.Errorf("expected transient part, got %s", sdkContent.Parts[1].Text)
	}
}

func TestPart_ToSDK_LazyHydration_NoInlineData(t *testing.T) {
	assetID := "test-asset"
	assetData := []byte("image-data")
	p := &llm.Part{
		AssetID: assetID,
	}

	resolver := &mockResolver{
		resolveFunc: func(ctx context.Context, id string) ([]byte, error) {
			return assetData, nil
		},
	}

	sdkPart := toSDKPart(context.Background(), p, resolver)
	if sdkPart.InlineData == nil || !reflect.DeepEqual(sdkPart.InlineData.Data, assetData) {
		t.Error("failed to hydrate without pre-existing InlineData")
	}
}

func TestHydrateAsset_Errors(t *testing.T) {

	t.Run("Resolver Error", func(t *testing.T) {
		ctx := context.Background()
		p := &llm.Part{AssetID: "fail-me"}
		res := &genai.Part{}
		resolver := &mockResolver{
			resolveFunc: func(ctx context.Context, assetID string) ([]byte, error) {
				return nil, fmt.Errorf("asset not found")
			},
		}

		hydrateAsset(ctx, p, res, resolver)

		if res.InlineData != nil {
			t.Errorf("expected InlineData to remain nil on resolver error, got %v", res.InlineData)
		}
	})

	t.Run("Already Hydrated", func(t *testing.T) {
		ctx := context.Background()
		p := &llm.Part{AssetID: "id1"}
		res := &genai.Part{
			InlineData: &genai.Blob{
				Data: []byte("already-here"),
			},
		}

		resolverCalled := false
		resolver := &mockResolver{
			resolveFunc: func(ctx context.Context, assetID string) ([]byte, error) {
				resolverCalled = true
				return []byte("new-data"), nil
			},
		}

		hydrateAsset(ctx, p, res, resolver)

		if resolverCalled {
			t.Error("resolver should not have been called for already hydrated part")
		}
		if string(res.InlineData.Data) != "already-here" {
			t.Errorf("expected already-here, got %s", string(res.InlineData.Data))
		}
	})
}

func TestToSDKContent_MixedUserTurn_OrdersInlineDataFirst(t *testing.T) {
	c := &llm.Content{Role: "user", Parts: []*llm.Part{
		{FunctionResponse: &llm.FunctionResponse{ID: "f0", Name: "tool_a", Response: map[string]interface{}{"result": "r0"}}},
		{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("b0")}},
		{FunctionResponse: &llm.FunctionResponse{ID: "f1", Name: "tool_b", Response: map[string]interface{}{"result": "r1"}}},
		{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("b1")}},
	}}

	parts := toSDKContent(context.Background(), c, nil).Parts

	if len(parts) != 4 {
		t.Fatalf("expected 4 parts, got %d", len(parts))
	}
	if parts[0].InlineData == nil || parts[0].FunctionResponse != nil {
		t.Errorf("parts[0]: expected InlineData only, got %+v", parts[0])
	}
	if parts[1].InlineData == nil || parts[1].FunctionResponse != nil {
		t.Errorf("parts[1]: expected InlineData only, got %+v", parts[1])
	}
	if parts[2].FunctionResponse == nil || parts[2].InlineData != nil {
		t.Errorf("parts[2]: expected FunctionResponse only, got %+v", parts[2])
	}
	if parts[3].FunctionResponse == nil || parts[3].InlineData != nil {
		t.Errorf("parts[3]: expected FunctionResponse only, got %+v", parts[3])
	}
	if string(parts[0].InlineData.Data) != "b0" {
		t.Errorf("parts[0]: expected data %q, got %q", "b0", parts[0].InlineData.Data)
	}
	if parts[2].FunctionResponse.ID != "f0" {
		t.Errorf("parts[2]: expected FR ID %q, got %q", "f0", parts[2].FunctionResponse.ID)
	}
	if parts[3].FunctionResponse.ID != "f1" {
		t.Errorf("parts[3]: expected FR ID %q, got %q", "f1", parts[3].FunctionResponse.ID)
	}
}

func TestToSDKContent_PoisonedPersistedShape_Normalizes(t *testing.T) {
	const (
		asset0 = "asset-0"
		asset1 = "asset-1"
	)
	blob0 := []byte("resolved-0")
	blob1 := []byte("resolved-1")
	resolver := &mockResolver{
		resolveFunc: func(ctx context.Context, assetID string) ([]byte, error) {
			switch assetID {
			case asset0:
				return blob0, nil
			case asset1:
				return blob1, nil
			default:
				return nil, nil
			}
		},
	}

	// Blob parts in the persisted shape: AssetID set + InlineData with nil
	// Data (exactly what prepareForStorage writes and Load re-parses).
	c := &llm.Content{Role: "user", Parts: []*llm.Part{
		{FunctionResponse: &llm.FunctionResponse{ID: "f0", Name: "tool_a", Response: map[string]interface{}{"result": "r0"}}},
		{AssetID: asset0, InlineData: &llm.Blob{MIMEType: "image/png"}},
		{FunctionResponse: &llm.FunctionResponse{ID: "f1", Name: "tool_b", Response: map[string]interface{}{"result": "r1"}}},
		{AssetID: asset1, InlineData: &llm.Blob{MIMEType: "image/png"}},
	}}

	parts := toSDKContent(context.Background(), c, resolver).Parts

	if len(parts) != 4 {
		t.Fatalf("expected 4 parts, got %d", len(parts))
	}
	// Order: [blob0, blob1, FR0, FR1].
	if parts[0].InlineData == nil || parts[0].FunctionResponse != nil {
		t.Errorf("parts[0]: expected InlineData only, got %+v", parts[0])
	}
	if parts[1].InlineData == nil || parts[1].FunctionResponse != nil {
		t.Errorf("parts[1]: expected InlineData only, got %+v", parts[1])
	}
	if parts[2].FunctionResponse == nil || parts[2].FunctionResponse.ID != "f0" {
		t.Errorf("parts[2]: expected FR f0, got %+v", parts[2])
	}
	if parts[3].FunctionResponse == nil || parts[3].FunctionResponse.ID != "f1" {
		t.Errorf("parts[3]: expected FR f1, got %+v", parts[3])
	}
	// Hydration proof: classification ran on the post-hydration wire shape,
	// not the persisted nil-Data shape — the InlineData blobs must carry the
	// resolver bytes for their AssetIDs.
	if string(parts[0].InlineData.Data) != "resolved-0" {
		t.Errorf("parts[0]: expected hydrated data %q, got %q", blob0, parts[0].InlineData.Data)
	}
	if string(parts[1].InlineData.Data) != "resolved-1" {
		t.Errorf("parts[1]: expected hydrated data %q, got %q", blob1, parts[1].InlineData.Data)
	}
}

func TestToSDKContent_OtherPartsMoveToEnd(t *testing.T) {
	tests := []struct {
		name    string
		parts   []*llm.Part
		wantLen int
		check   func(t *testing.T, parts []*genai.Part)
	}{
		{
			name: "warning before blob and FR",
			parts: []*llm.Part{
				{Text: "w"},
				{FunctionResponse: &llm.FunctionResponse{ID: "f0", Name: "tool_a", Response: map[string]interface{}{"result": "r0"}}},
				{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("b0")}},
			},
			wantLen: 3,
			check: func(t *testing.T, parts []*genai.Part) {
				if parts[0].InlineData == nil || parts[0].FunctionResponse != nil {
					t.Errorf("parts[0]: expected InlineData only, got %+v", parts[0])
				}
				if parts[1].FunctionResponse == nil || parts[1].InlineData != nil {
					t.Errorf("parts[1]: expected FunctionResponse only, got %+v", parts[1])
				}
				if parts[2].Text != "w" {
					t.Errorf("parts[2]: expected warning text %q, got %q", "w", parts[2].Text)
				}
			},
		},
		{
			name: "interleaved warning, blobs and FRs",
			parts: []*llm.Part{
				{FunctionResponse: &llm.FunctionResponse{ID: "f0", Name: "tool_a", Response: map[string]interface{}{"result": "r0"}}},
				{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("b0")}},
				{Text: "w"},
				{FunctionResponse: &llm.FunctionResponse{ID: "f1", Name: "tool_b", Response: map[string]interface{}{"result": "r1"}}},
				{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("b1")}},
			},
			wantLen: 5,
			check: func(t *testing.T, parts []*genai.Part) {
				if parts[0].InlineData == nil || parts[1].InlineData == nil {
					t.Errorf("parts[0..1]: expected InlineData, got %+v / %+v", parts[0], parts[1])
				}
				if parts[2].FunctionResponse == nil || parts[2].FunctionResponse.ID != "f0" {
					t.Errorf("parts[2]: expected FR f0, got %+v", parts[2])
				}
				if parts[3].FunctionResponse == nil || parts[3].FunctionResponse.ID != "f1" {
					t.Errorf("parts[3]: expected FR f1, got %+v", parts[3])
				}
				if parts[4].Text != "w" {
					t.Errorf("parts[4]: expected warning text %q, got %q", "w", parts[4].Text)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &llm.Content{Role: "user", Parts: tt.parts}
			parts := toSDKContent(context.Background(), c, nil).Parts
			if len(parts) != tt.wantLen {
				t.Fatalf("expected %d parts, got %d", tt.wantLen, len(parts))
			}
			tt.check(t, parts)
		})
	}
}

func TestToSDKContent_ModelRole_MixedParts_NoReorder(t *testing.T) {
	// The role gate must leave every non-user role untouched, even when the
	// content mixes FunctionResponse with InlineData (FR before blob).
	tests := []struct {
		name string
		role string
	}{
		{name: "model role", role: "model"},
		{name: "system role", role: "system"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &llm.Content{Role: tt.role, Parts: []*llm.Part{
				{FunctionResponse: &llm.FunctionResponse{ID: "f0", Name: "tool_a", Response: map[string]interface{}{"result": "r0"}}},
				{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("b0")}},
			}}

			parts := toSDKContent(context.Background(), c, nil).Parts

			if len(parts) != 2 {
				t.Fatalf("expected 2 parts, got %d", len(parts))
			}
			// Slice order must be IDENTICAL to input: FR stays BEFORE the blob.
			if parts[0].FunctionResponse == nil {
				t.Errorf("parts[0]: expected FunctionResponse (no reorder for role %q), got %+v", tt.role, parts[0])
			}
			if parts[1].InlineData == nil {
				t.Errorf("parts[1]: expected InlineData (no reorder for role %q), got %+v", tt.role, parts[1])
			}
		})
	}
}
