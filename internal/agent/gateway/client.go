// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package gateway

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ResilientClient wraps an LLMClient with retry logic and domain-specific error wrapping.
type ResilientClient struct {
	client           llm.LLMClient
	disableStreaming bool
}

// NewResilientClient creates a new ResilientClient.
func NewResilientClient(client llm.LLMClient, disableStreaming bool) *ResilientClient {
	return &ResilientClient{
		client:           client,
		disableStreaming: disableStreaming,
	}
}

// httpStatusErr captures various HTTP error implementations in SDKs.
type httpStatusErr interface {
	StatusCode() int
}

// errorClassifier defines a function that attempts to classify an error into domain types.
type errorClassifier func(error) (error, bool)

var defaultClassifiers = []errorClassifier{
	classifyDomain,
	classifyGRPC,
	classifyHTTP,
	classifyString,
}

// WrapError converts raw client errors into domain-specific Gateway errors.
// It uses a Chain of Responsibility pattern for extensibility and low complexity.
func (r *ResilientClient) WrapError(err error) error {
	if err == nil {
		return nil
	}

	for _, classify := range defaultClassifiers {
		if wrapped, ok := classify(err); ok {
			return wrapped
		}
	}

	return fmt.Errorf("%w: %v", ErrTerminal, err)
}

func classifyDomain(err error) (error, bool) {
	if errors.Is(err, ErrAuth) || errors.Is(err, ErrTransient) || errors.Is(err, ErrTerminal) {
		return err, true
	}
	return nil, false
}

func classifyGRPC(err error) (error, bool) {
	if s, ok := status.FromError(err); ok {
		switch s.Code() {
		case codes.Unauthenticated:
			return fmt.Errorf("%w: %v", ErrAuth, err), true
		case codes.ResourceExhausted, codes.Unavailable, codes.DeadlineExceeded, codes.Aborted:
			return fmt.Errorf("%w: %v", ErrTransient, err), true
		case codes.PermissionDenied, codes.InvalidArgument:
			return fmt.Errorf("%w: %v", ErrTerminal, err), true
		}
	}
	return nil, false
}

func classifyHTTP(err error) (error, bool) {
	var httpErr httpStatusErr
	if errors.As(err, &httpErr) {
		code := httpErr.StatusCode()
		switch {
		case code == 401:
			return fmt.Errorf("%w: %v", ErrAuth, err), true
		case code == 429 || code >= 500:
			return fmt.Errorf("%w: %v", ErrTransient, err), true
		case code >= 400 && code < 500:
			return fmt.Errorf("%w: %v", ErrTerminal, err), true
		}
	}
	return nil, false
}

func classifyString(err error) (error, bool) {
	msg := strings.ToUpper(err.Error())
	if strings.Contains(msg, "UNAUTHENTICATED") || strings.Contains(msg, "API_KEY_INVALID") {
		return fmt.Errorf("%w: %v", ErrAuth, err), true
	}
	return nil, false
}

type result struct {
	content *llm.Content
	metrics *llm.Metrics
	err     error
}

// Generate handles the LLM interaction logic, returning a stream and a finalizer.
func (r *ResilientClient) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
	outCh := make(chan *llm.Content, 100)
	resCh := make(chan result, 1)

	go func() {
		content, metrics, err := r.executeWithTransparentRetry(ctx, input, tools, resolver, outCh)
		close(outCh)
		resCh <- result{content, metrics, err}
	}()

	finalize := func() (*llm.Content, *llm.Metrics, error) {
		select {
		case res := <-resCh:
			return res.content, res.metrics, res.err
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}

	return outCh, finalize
}

// executeWithTransparentRetry only retries for things the client can fix (like Auth)
func (r *ResilientClient) executeWithTransparentRetry(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, outCh chan<- *llm.Content) (*llm.Content, *llm.Metrics, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		content, metrics, err := r.attemptCall(ctx, input, tools, resolver, outCh)
		if err == nil {
			return content, metrics, nil
		}

		wrapped := r.WrapError(err)
		if errors.Is(wrapped, ErrAuth) {
			if refreshErr := r.client.RefreshAuth(); refreshErr == nil {
				continue // Fixed! Retry once.
			}
		}
		lastErr = wrapped
		break // Let the TurnEngine handle transient/terminal retries
	}
	return nil, nil, lastErr
}

func (r *ResilientClient) attemptCall(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, outCh chan<- *llm.Content) (*llm.Content, *llm.Metrics, error) {
	if r.disableStreaming {
		content, metrics, err := r.client.SendChat(ctx, input, tools, resolver)
		if err == nil {
			select {
			case outCh <- content:
			case <-ctx.Done():
			}
		}
		return content, metrics, err
	}
	return r.performStreamingCall(ctx, input, tools, resolver, outCh)
}

func (r *ResilientClient) performStreamingCall(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, outCh chan<- *llm.Content) (*llm.Content, *llm.Metrics, error) {
	finalContent := &llm.Content{Role: "model"}
	callback := func(c *llm.Content) {
		for _, p := range c.Parts {
			finalContent.AddPart(p)
		}
		select {
		case outCh <- c:
		case <-ctx.Done():
		}
	}
	metrics, err := r.client.StreamChat(ctx, input, tools, resolver, callback)
	return finalContent, metrics, err
}

// SetSystemInstructions updates the system instructions in the underlying LLM client.
func (r *ResilientClient) SetSystemInstructions(instr string) {
	r.client.SetSystemInstructions(instr)
}

// SendChat delegates to the underlying client.
func (r *ResilientClient) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	return r.client.SendChat(ctx, history, tools, resolver)
}

// StreamChat delegates to the underlying client.
func (r *ResilientClient) StreamChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
	return r.client.StreamChat(ctx, history, tools, resolver, callback)
}

// RefreshAuth delegates to the underlying client.
func (r *ResilientClient) RefreshAuth() error {
	return r.client.RefreshAuth()
}

// GenerateImages delegates to the underlying client.
func (r *ResilientClient) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return r.client.GenerateImages(ctx, model, prompt, mimeType)
}
