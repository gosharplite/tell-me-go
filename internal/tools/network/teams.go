// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package network

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/types"
)

type teamsManager struct {
	sm *security.SecurityManager
}

// RegisterTeams adds Teams-related tools to the registry.
func RegisterTeams(r *registry.Registry, sm *security.SecurityManager) {
	m := &teamsManager{sm: sm}
	r.RegisterWithOptions(&types.ToolDeclaration{
		Name:        "send_teams_message",
		Description: "Sends a message to a Microsoft Teams channel using a Power Automate workflow webhook.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"webhook_url": {
					Type:        "STRING",
					Description: "The Power Automate workflow URL (must start with https://).",
				},
				"message": {
					Type:        "STRING",
					Description: "The message to send to the channel.",
				},
				"reason": {
					Type:        "STRING",
					Description: "Reason for sending this message.",
				},
			},
			Required: []string{"webhook_url", "message", "reason"},
		},
	}, m.sendTeamsMessage, registry.ToolOptions{Serial: true})
}

func (m *teamsManager) sendTeamsMessage(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		WebhookURL string `json:"webhook_url"`
		Message    string `json:"message"`
		Reason     string `json:"reason"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	webhookURL := params.WebhookURL
	message := params.Message

	if webhookURL == "" || message == "" {
		return types.ToolResult{}, fmt.Errorf("webhook_url and message are required")
	}

	// Safety confirmation (unless bypassed)
	detail := fmt.Sprintf("Reason: %s\n\nMessage:\n%s", params.Reason, message)
	approved, err := m.sm.ConfirmDestructiveAction(ctx, "send message to Teams", webhookURL, detail)
	if err != nil {
		return types.ToolResult{}, err
	}
	if !approved {
		return types.ToolResult{Text: "Action cancelled by user."}, nil
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
		return types.ToolResult{}, fmt.Errorf("failed to marshal payload: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return types.ToolResult{}, fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return types.ToolResult{Text: fmt.Sprintf("Successfully sent message to Teams. Status: %s", resp.Status)}, nil
	}

	return types.ToolResult{Text: fmt.Sprintf("Failed to send message to Teams. Status: %s", resp.Status)}, nil
}
