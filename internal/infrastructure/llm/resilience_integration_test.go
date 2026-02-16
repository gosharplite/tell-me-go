// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/llm/openai"
)

func TestResilientClient_OpenAI_Classification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("Rate limit exceeded"))
	}))
	defer server.Close()

	innerClient := openai.NewClient(server.URL, "gpt-4", &auth.BearerAuth{Token: "key"}, nil, "")
	// NewResilientClient is in the same package (llm)
	client := NewResilientClient(innerClient, true) // disableStreaming=true makes it use SendChat internally

	_, finalize := client.Generate(context.Background(), nil, nil, nil)
	_, _, err := finalize()
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, llm.ErrTransient) {
		t.Errorf("expected llm.ErrTransient, got %v", err)
	}

	if !strings.Contains(err.Error(), "429") {
		t.Errorf("expected error to contain 429, got %v", err)
	}
}
