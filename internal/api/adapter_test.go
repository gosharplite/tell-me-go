// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package api

import (
	"context"
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
	}

	internalPart := FromSDKPart(sdkPart)

	if internalPart.Text != sdkPart.Text {
		t.Errorf("expected text %s, got %s", sdkPart.Text, internalPart.Text)
	}
	if internalPart.Thought != sdkPart.Thought {
		t.Errorf("expected thought %v, got %v", sdkPart.Thought, internalPart.Thought)
	}
	if !reflect.DeepEqual(internalPart.ThoughtSignature, sdkPart.ThoughtSignature) {
		t.Errorf("expected signature %v, got %v", sdkPart.ThoughtSignature, internalPart.ThoughtSignature)
	}

	backToSDK := ToSDKPart(context.Background(), internalPart, nil)
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

	sdkPart := ToSDKPart(context.Background(), p, resolver)

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
				Thought:          true,
				ThoughtSignature: []byte("sig"),
			},
		},
	}

	sdkContent := ToSDKContent(context.Background(), content, nil)
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

	internalPart := FromSDKPart(sdkPart)
	if internalPart.FunctionCall.Name != "test_tool" {
		t.Errorf("expected test_tool, got %s", internalPart.FunctionCall.Name)
	}

	backToSDK := ToSDKPart(context.Background(), internalPart, nil)
	if backToSDK.FunctionCall.Name != "test_tool" {
		t.Errorf("roundtrip failed for function call")
	}

	sdkPartResp := &genai.Part{
		FunctionResponse: &genai.FunctionResponse{
			Name:     "test_tool",
			Response: map[string]interface{}{"result": "ok"},
		},
	}
	internalPartResp := FromSDKPart(sdkPartResp)
	if internalPartResp.FunctionResponse.Name != "test_tool" {
		t.Errorf("expected test_tool, got %s", internalPartResp.FunctionResponse.Name)
	}

	backToSDKResp := ToSDKPart(context.Background(), internalPartResp, nil)
	if backToSDKResp.FunctionResponse.Name != "test_tool" {
		t.Errorf("roundtrip failed for function response")
	}
}

func TestContent_Conversion_Nil(t *testing.T) {
	if ToSDKContent(context.Background(), nil, nil) != nil {
		t.Error("ToSDKContent(nil) should be nil")
	}
	if FromSDKContent(nil) != nil {
		t.Error("FromSDKContent(nil) should be nil")
	}
	if ToSDKPart(context.Background(), nil, nil) != nil {
		t.Error("ToSDKPart(nil) should be nil")
	}
	if FromSDKPart(nil) != nil {
		t.Error("FromSDKPart(nil) should be nil")
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

	sdkContent := ToSDKContent(context.Background(), content, nil)
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

	sdkPart := ToSDKPart(context.Background(), p, resolver)
	if sdkPart.InlineData == nil || !reflect.DeepEqual(sdkPart.InlineData.Data, assetData) {
		t.Error("failed to hydrate without pre-existing InlineData")
	}
}
