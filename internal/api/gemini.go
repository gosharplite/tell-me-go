// Package api handles communication with the Gemini API.
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Client represents a Gemini API client.
type Client struct {
	URL    string
	Model  string
	APIKey string
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
func NewClient(url, model, apiKey string) *Client {
	return &Client{
		URL:    url,
		Model:  model,
		APIKey: apiKey,
	}
}

// SendMessage sends a single prompt to the Gemini API and returns the text response.
func (c *Client) SendMessage(prompt string) (string, error) {
	apiURL := fmt.Sprintf("%s/%s:generateContent?key=%s", c.URL, c.Model, c.APIKey)

	reqPayload := Request{
		Contents: []Content{
			{
				Parts: []Part{{Text: prompt}},
			},
		},
	}

	jsonData, err := json.Marshal(reqPayload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := http.Post(apiURL, "application/json", bytes.NewBuffer(jsonData))
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
