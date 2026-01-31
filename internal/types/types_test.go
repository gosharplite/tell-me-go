// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package types

import (
	"reflect"
	"testing"

	"google.golang.org/genai"
)

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

	backToSDK := internalPart.ToSDK()
	if !reflect.DeepEqual(backToSDK, sdkPart) {
		t.Errorf("roundtrip failed: expected %+v, got %+v", sdkPart, backToSDK)
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

	sdkContent := content.ToSDK()
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
