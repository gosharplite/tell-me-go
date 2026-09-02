// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package gemini

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
	"google.golang.org/genai"
)

// wireContent mirrors the Gemini generateContent wire shape for a single
// content entry (role + parts), decoding only the fields the round-trip test
// pins: inlineData (base64), functionResponse, and functionCall.
type wireContent struct {
	Role  string `json:"role"`
	Parts []struct {
		InlineData *struct {
			MimeType string `json:"mimeType"`
			Data     string `json:"data"` // base64
		} `json:"inlineData"`
		FunctionResponse *struct {
			ID       string         `json:"id"`
			Name     string         `json:"name"`
			Response map[string]any `json:"response"`
		} `json:"functionResponse"`
		FunctionCall *struct {
			ID   string         `json:"id"`
			Name string         `json:"name"`
			Args map[string]any `json:"args"`
		} `json:"functionCall"`
	} `json:"parts"`
}

type wireRequest struct {
	Contents []wireContent `json:"contents"`
}

// TestSendChat_MixedMediaStoreRoundTrip pins the full persistence → reload →
// hydration → wire-ordering chain for mixed-media user turns. Each scenario
// seeds a different persisted part order (same blob bytes so fingerprints are
// comparable); the wire must converge to the canonical
// [model FC][user inline0 inline1 FR0 FR1] order regardless of the seed.
func TestSendChat_MixedMediaStoreRoundTrip(t *testing.T) {
	blob0 := &llm.Part{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("blob0-data")}}
	blob1 := &llm.Part{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("blob1-data")}}
	fr0 := &llm.Part{FunctionResponse: &llm.FunctionResponse{ID: "f0", Name: "tool_a", Response: map[string]interface{}{"result": "r0"}}}
	fr1 := &llm.Part{FunctionResponse: &llm.FunctionResponse{ID: "f1", Name: "tool_b", Response: map[string]interface{}{"result": "r1"}}}

	// Canonical wire after normalization (base64-decoded blob bytes embedded).
	const expectedFingerprint = "model|fc:tool_x|user|inline:blob0-data|inline:blob1-data|fr:f0:tool_a:r0|fr:f1:tool_b:r1"

	tests := []struct {
		name  string
		parts []*llm.Part
	}{
		{name: "A_pre1442_poisoned_interleaved", parts: []*llm.Part{fr0, blob0, fr1, blob1}},
		{name: "B_fresh_canonical", parts: []*llm.Part{blob0, blob1, fr0, fr1}},
		{name: "C_post1442_poisoned_adjacent", parts: []*llm.Part{blob0, fr0, blob1, fr1}},
	}

	fingerprints := make(map[string]string, len(tests))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			tmpDir := t.TempDir()
			historyFile := filepath.Join(tmpDir, "history.jsonl")
			archiveFile := filepath.Join(tmpDir, "history.archive.jsonl")
			fs := infrapersistence.NewOSFileSystem()
			m := history.NewManagerWithAssetStore(fs, persistencetest.NewAssetStore(fs, filepath.Join(filepath.Dir(historyFile), "assets")), historyFile, archiveFile)

			// Seed: model turn (single FunctionCall) then user turn with the
			// scenario's part order. AddContent → prepareForStorage moves the
			// blob bytes into the real asset store, sets AssetID, and nulls
			// InlineData.Data — the true persisted shape.
			if err := m.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{
				{FunctionCall: &llm.FunctionCall{ID: "fc1", Name: "tool_x", Args: map[string]interface{}{"q": 1}}},
			}}); err != nil {
				t.Fatalf("AddContent(model turn): %v", err)
			}
			if err := m.AddContent(ctx, &llm.Content{Role: "user", Parts: tt.parts}); err != nil {
				t.Fatalf("AddContent(user turn): %v", err)
			}

			// Reload from disk: Load re-parses the persisted JSONL where the
			// blob parts carry AssetID + InlineData{MIMEType} with nil Data.
			mfs0 := infrapersistence.NewOSFileSystem()
			m2 := history.NewManagerWithAssetStore(mfs0, persistencetest.NewAssetStore(mfs0, filepath.Join(filepath.Dir(historyFile), "assets")), historyFile, archiveFile)
			if err := m2.Load(ctx); err != nil {
				t.Fatalf("Load: %v", err)
			}
			window, err := m2.GetWindow(ctx, 0, -1)
			if err != nil {
				t.Fatalf("GetWindow: %v", err)
			}

			// Buffered (cap 1): the handler always records the fingerprint, so
			// the post-SendChat read can never block. Channel hand-off is the
			// synchronization — no time.Sleep (ADR-036).
			fpCh := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fpCh <- validateMixedMediaRoundTripReq(t, r)
				if err := json.NewEncoder(w).Encode(genai.GenerateContentResponse{
					Candidates: []*genai.Candidate{{Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: "OK"}}}}},
				}); err != nil {
					t.Errorf("failed to encode mock response: %v", err)
				}
			}))
			t.Cleanup(server.Close)

			apiURL := server.URL + "/aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
			bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
			eventstest.CleanupBus(t, bus)
			client, err := NewClient(apiURL, "test-model", &auth.BearerAuth{Token: "test-token"}, WithEventBus(bus), WithTimeout(5*time.Second))
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}

			content, _, err := client.SendChat(ctx, window, nil, m2.GetResolver())
			if err != nil {
				t.Fatalf("SendChat: %v", err)
			}
			if len(content.Parts) == 0 || content.Parts[0].Text != "OK" {
				t.Errorf("expected response text %q, got %+v", "OK", content.Parts)
			}

			fingerprints[tt.name] = <-fpCh
		})
	}

	// Joint convergence + idempotence: every seed must produce the identical
	// canonical wire. B was seeded canonical, so B == expected proves the
	// normalization is idempotent; A (pre-#1442) and C (post-#1442) poisoned
	// shapes must converge to the same wire.
	for _, tt := range tests {
		got, ok := fingerprints[tt.name]
		if !ok || got == "" {
			t.Errorf("scenario %s: no fingerprint recorded", tt.name)
			continue
		}
		if got != expectedFingerprint {
			t.Errorf("scenario %s: wire fingerprint %q, want %q", tt.name, got, expectedFingerprint)
		}
	}
}

// validateMixedMediaRoundTripReq decodes the generateContent wire request and
// asserts the canonical [model FC][user inline0 inline1 FR0 FR1] order plus the
// base64 hydration proof. It returns the canonical fingerprint (base64-decoded
// blob bytes embedded). It uses t.Errorf only (never t.Fatal) so the handler
// always completes and records the fingerprint.
func validateMixedMediaRoundTripReq(t *testing.T, r *http.Request) string {
	t.Helper()
	if r.Header.Get("Authorization") != "Bearer test-token" {
		t.Errorf("expected bearer token, got %q", r.Header.Get("Authorization"))
	}

	var req wireRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Errorf("failed to decode request body: %v", err)
		return ""
	}

	// 1. Exactly two contents: model turn then user turn.
	if len(req.Contents) != 2 {
		t.Errorf("expected 2 contents, got %d", len(req.Contents))
	} else {
		if req.Contents[0].Role != "model" {
			t.Errorf("contents[0]: expected role %q, got %q", "model", req.Contents[0].Role)
		}
		if req.Contents[1].Role != "user" {
			t.Errorf("contents[1]: expected role %q, got %q", "user", req.Contents[1].Role)
		}
	}

	// 2. Model turn untouched: exactly one FunctionCall part, no reorder.
	if len(req.Contents) > 0 {
		modelParts := req.Contents[0].Parts
		if len(modelParts) != 1 {
			t.Errorf("model turn: expected 1 part, got %d", len(modelParts))
		} else if modelParts[0].FunctionCall == nil {
			t.Errorf("model turn parts[0]: expected FunctionCall, got %+v", modelParts[0])
		} else if modelParts[0].FunctionCall.ID != "fc1" || modelParts[0].FunctionCall.Name != "tool_x" {
			t.Errorf("model turn parts[0]: expected FunctionCall fc1/tool_x, got %+v", modelParts[0].FunctionCall)
		}
	}

	// 3. User turn: exactly 4 parts in canonical order [inline, inline, FR, FR].
	if len(req.Contents) > 1 {
		userParts := req.Contents[1].Parts
		if len(userParts) != 4 {
			t.Errorf("user turn: expected 4 parts, got %d", len(userParts))
		} else {
			for i := 0; i < 2; i++ {
				if userParts[i].InlineData == nil || userParts[i].FunctionResponse != nil {
					t.Errorf("user turn parts[%d]: expected InlineData only, got %+v", i, userParts[i])
				}
			}
			for i := 2; i < 4; i++ {
				if userParts[i].FunctionResponse == nil || userParts[i].InlineData != nil {
					t.Errorf("user turn parts[%d]: expected FunctionResponse only, got %+v", i, userParts[i])
				}
			}

			// 4. Base64 hydration proof: prepareForStorage nulled Data in the
			// persisted JSONL; the GetResolver → jsonlStore.Resolve →
			// assetStore.Get → hydrateAsset chain must have restored it.
			for i, want := range []string{"blob0-data", "blob1-data"} {
				if userParts[i].InlineData == nil {
					continue
				}
				data, err := base64.StdEncoding.DecodeString(userParts[i].InlineData.Data)
				if err != nil {
					t.Errorf("user turn parts[%d]: invalid base64 inline data: %v", i, err)
				} else if string(data) != want {
					t.Errorf("user turn parts[%d]: expected hydrated data %q, got %q", i, want, string(data))
				}
			}

			// 5. FR identity in order.
			fr := userParts[2].FunctionResponse
			if fr == nil || fr.ID != "f0" || fr.Name != "tool_a" || fr.Response["result"] != "r0" {
				t.Errorf("user turn parts[2]: expected FR f0/tool_a/r0, got %+v", fr)
			}
			fr = userParts[3].FunctionResponse
			if fr == nil || fr.ID != "f1" || fr.Name != "tool_b" || fr.Response["result"] != "r1" {
				t.Errorf("user turn parts[3]: expected FR f1/tool_b/r1, got %+v", fr)
			}
		}
	}

	// Canonical fingerprint with base64-decoded blob bytes embedded.
	fp := ""
	for i, c := range req.Contents {
		if i > 0 {
			fp += "|"
		}
		fp += c.Role
		for _, p := range c.Parts {
			switch {
			case p.FunctionCall != nil:
				fp += "|fc:" + p.FunctionCall.Name
			case p.InlineData != nil:
				data, _ := base64.StdEncoding.DecodeString(p.InlineData.Data)
				fp += "|inline:" + string(data)
			case p.FunctionResponse != nil:
				result, _ := p.FunctionResponse.Response["result"].(string)
				fp += "|fr:" + p.FunctionResponse.ID + ":" + p.FunctionResponse.Name + ":" + result
			}
		}
	}
	return fp
}
