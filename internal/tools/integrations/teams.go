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

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

type teamsManager struct {
	sm     *security.SecurityManager
	client tools.HTTPClient
}

func NewTeamsManager(sm *security.SecurityManager, client tools.HTTPClient) *teamsManager {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &teamsManager{sm: sm, client: client}
}

func (m *teamsManager) sendTeamsMessage(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		WebhookURL string `json:"webhook_url"`
		Message    string `json:"message"`
		Reason     string `json:"reason"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
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
	detail := fmt.Sprintf("Reason: %s\n\nMessage:\n%s", params.Reason, message)
	approved, err := m.sm.ConfirmDestructiveAction(ctx, "send message to Teams", webhookURL, detail)
	if err != nil {
		return tools.ToolResult{}, err
	}
	if !approved {
		return tools.ToolResult{Text: "Action cancelled by user."}, nil
	}

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
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("failed to send message to Teams, status: %s", resp.Status)
	}

	return resp.Status, nil
}
