// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package types

import (
	"reflect"
	"testing"

	"google.golang.org/genai"
)

type mockResolver struct {
	resolveFunc func(assetID string) ([]byte, error)
}

func (m *mockResolver) Resolve(assetID string) ([]byte, error) {
	return m.resolveFunc(assetID)
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

	backToSDK := internalPart.ToSDK(nil)
	if !reflect.DeepEqual(backToSDK, sdkPart) {
		t.Errorf("roundtrip failed: expected %+v, got %+v", sdkPart, backToSDK)
	}
}

func TestPart_ToSDK_LazyHydration(t *testing.T) {
	assetID := "test-asset"
	assetData := []byte("image-data")
	p := &Part{
		AssetID: assetID,
		InlineData: &Blob{
			MIMEType: "image/png",
		},
	}

	resolver := &mockResolver{
		resolveFunc: func(id string) ([]byte, error) {
			if id == assetID {
				return assetData, nil
			}
			return nil, nil
		},
	}

	sdkPart := p.ToSDK(resolver)

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
	content := &Content{
		Role: "model",
		Parts: []*Part{
			{
				Text:             "thinking",
				Thought:          true,
				ThoughtSignature: []byte("sig"),
			},
		},
	}

	sdkContent := content.ToSDK(nil)
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
