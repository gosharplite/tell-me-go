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
	Contents []Content `json:"contents"`
}

type Content struct {
	Role  string `json:"role,omitempty"`
	Parts []Part `json:"parts"`
}

type Part struct {
	Text string `json:"text"`
}

// Response represents the Gemini API response payload.
type Response struct {
	Candidates []Candidate `json:"candidates"`
}

type Candidate struct {
	Content Content `json:"content"`
}

// NewClient returns a new Gemini API client.
func NewClient(url, model string, authenticator auth.Authenticator) *Client {
	return &Client{
		URL:           url,
		Model:         model,
		Authenticator: authenticator,
	}
}

// SendChat sends the conversation history to the Gemini API and returns the model's text response.
func (c *Client) SendChat(history []Content) (string, error) {
	// 1. Prepare Base URL
	u, err := url.Parse(fmt.Sprintf("%s/%s:generateContent", c.URL, c.Model))
	if err != nil {
		return "", fmt.Errorf("failed to parse url: %w", err)
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
	}

	jsonData, err := json.Marshal(reqPayload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// 4. Execute Request
	httpReq, err := http.NewRequest("POST", u.String(), bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Apply Headers
	for k, v := range authReq.Headers {
		httpReq.Header.Set(k, v)
	}

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("api request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var apiResp Response
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(apiResp.Candidates) == 0 || len(apiResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty response from api")
	}

	return apiResp.Candidates[0].Content.Parts[0].Text, nil
}
