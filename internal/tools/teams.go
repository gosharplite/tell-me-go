// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"google.golang.org/genai"
)

type teamsManager struct {
	sm *SecurityManager
}

// RegisterTeamsTools adds Teams-related tools to the registry.
func RegisterTeamsTools(r *Registry, sm *SecurityManager) {
	m := &teamsManager{sm: sm}
	r.RegisterWithOptions(&genai.FunctionDeclaration{
		Name:        "send_teams_message",
		Description: "Sends a message to a Microsoft Teams channel using a Power Automate workflow webhook.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"webhook_url": {
					Type:        genai.TypeString,
					Description: "The Power Automate workflow URL (must start with https://).",
				},
				"message": {
					Type:        genai.TypeString,
					Description: "The message to send to the channel.",
				},
			},
			Required: []string{"webhook_url", "message"},
		},
	}, m.sendTeamsMessage, ToolOptions{Serial: true})
}

func (m *teamsManager) sendTeamsMessage(ctx context.Context, args map[string]interface{}) (string, error) {
	webhookURL, _ := args["webhook_url"].(string)
	message, _ := args["message"].(string)

	if webhookURL == "" || message == "" {
		return "", fmt.Errorf("webhook_url and message are required")
	}

	// Safety confirmation (unless bypassed)
	if !m.sm.ConfirmDestructiveAction("send message to Teams", webhookURL, message) {
		return "Action cancelled by user.", nil
	}

	payload := map[string]interface{}{
		"type":    "AdaptiveCard",
		"version": "1.2",
		"body": []map[string]interface{}{
			{
				"type": "TextBlock",
				"text": message,
				"wrap": true,
			},
		},
		"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return fmt.Sprintf("Successfully sent message to Teams. Status: %s", resp.Status), nil
	}

	return fmt.Sprintf("Failed to send message to Teams. Status: %s", resp.Status), nil
}
