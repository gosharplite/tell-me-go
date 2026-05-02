// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type teamsManager struct {
	sm     teamsSecurity
	client tools.HTTPClient
}

func newteamsManager(sm teamsSecurity, client tools.HTTPClient) *teamsManager {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &teamsManager{sm: sm, client: client}
}

func (m *teamsManager) sendTeamsMessage(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		WebhookURL string `json:"webhook_url"`
		Message    string `json:"message"`
		Reason     string `json:"reason"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	webhookURL := params.WebhookURL
	message := params.Message

	if webhookURL == "" || message == "" {
		return tools.ToolResult{}, fmt.Errorf("webhook_url and message are required")
	}
	if !strings.HasPrefix(webhookURL, "https://") {
		return tools.ToolResult{}, fmt.Errorf("webhook_url must start with https://")
	}

	// Safety confirmation (unless bypassed)

	body, err := buildTeamsRequestBody(message, params.Reason)
	if err != nil {
		return tools.ToolResult{}, err
	}

	status, err := m.postToWebhook(ctx, webhookURL, body)
	if err != nil {
		return tools.ToolResult{Text: err.Error()}, nil
	}

	return tools.ToolResult{Text: fmt.Sprintf("Successfully sent message to Teams. Status: %s", status)}, nil
}

func buildTeamsRequestBody(message, reason string) ([]byte, error) {
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
	return json.Marshal(payload)
}

func (m *teamsManager) postToWebhook(ctx context.Context, url string, body []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send message: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("failed to send message to Teams, status: %s", resp.Status)
	}

	return resp.Status, nil
}

type teamsSecurity interface {
	security.ActionConfirmer
}

func registerTeams(r tools.Registry, sm security.Manager, client tools.HTTPClient) error {
	m := newteamsManager(sm, client)
	if err := r.RegisterToToolkitWithOptions("teams", &tools.ToolDeclaration{
		Name:            "send_teams_message",
		Description:     "Sends a message to a Microsoft Teams channel using a Power Automate workflow webhook.",
		RequiresConsent: true,
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
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
	}, m.sendTeamsMessage, tools.ToolOptions{Serial: true}); err != nil {
		return err
	}
	return nil
}
