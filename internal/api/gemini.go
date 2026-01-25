// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package api handles communication with the Gemini API.
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/gosharplite/tell-me-go/internal/auth"
)

// Client represents a Gemini API client.
type Client struct {
	URL           string
	Model         string
	Authenticator auth.Authenticator
}

// Request represents the Gemini API request payload.
type Request struct {
	Contents []Content   `json:"contents"`
	Tools    interface{} `json:"tools,omitempty"`
}

type Content struct {
	Role  string `json:"role,omitempty"` // Strictly required by some providers, don't use omitempty in final payload if possible
	Parts []Part `json:"parts"`
}

type Part struct {
	Text             string            `json:"text,omitempty"`
	Thought          bool              `json:"thought,omitempty"`
	ThoughtSignature string            `json:"thoughtSignature,omitempty"`
	FunctionCall     *FunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *FunctionResponse `json:"functionResponse,omitempty"`
}

type FunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

type FunctionResponse struct {
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response"`
}

// Response represents the Gemini API response payload.
type Response struct {
	Candidates []Candidate `json:"candidates"`
}

type Candidate struct {
	Content      Content       `json:"content"`
	FinishReason string        `json:"finishReason"`
	SafetyRating []interface{} `json:"safetyRatings"`
}

// NewClient returns a new Gemini API client.
func NewClient(url, model string, authenticator auth.Authenticator) *Client {
	return &Client{
		URL:           url,
		Model:         model,
		Authenticator: authenticator,
	}
}

// SendChat sends the conversation history to the Gemini API and returns the full response content.
func (c *Client) SendChat(history []Content, tools interface{}) (*Content, error) {
	// 1. Prepare Base URL
	u, err := url.Parse(fmt.Sprintf("%s/%s:generateContent", c.URL, c.Model))
	if err != nil {
		return nil, fmt.Errorf("failed to parse url: %w", err)
	}

	// 2. Apply Authentication
	authReq := &auth.Request{
		QueryParams: make(map[string]string),
		Headers:     make(map[string]string),
	}
	c.Authenticator.Apply(authReq)

	// Add Query Params
	q := u.Query()
	for k, v := range authReq.QueryParams {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	// 3. Prepare Payload
	reqPayload := Request{
		Contents: history,
		Tools:    tools,
	}

	jsonData, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// 4. Execute Request
	httpReq, err := http.NewRequest("POST", u.String(), bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Apply Headers
	for k, v := range authReq.Headers {
		httpReq.Header.Set(k, v)
	}

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("api request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var apiResp Response
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(apiResp.Candidates) == 0 {
		return nil, fmt.Errorf("empty response from api")
	}

	return &apiResp.Candidates[0].Content, nil
}
