// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

type teamsManager struct {
	sm     *security.SecurityManager
	client tools.HTTPClient
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

	// Safety confirmation (unless bypassed)
	detail := fmt.Sprintf("Reason: %s\n\nMessage:\n%s", params.Reason, message)
	approved, err := m.sm.ConfirmDestructiveAction(ctx, "send message to Teams", webhookURL, detail)
	if err != nil {
		return tools.ToolResult{}, err
	}
	if !approved {
		return tools.ToolResult{Text: "Action cancelled by user."}, nil
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
		return tools.ToolResult{}, fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return tools.ToolResult{Text: fmt.Sprintf("Successfully sent message to Teams. Status: %s", resp.Status)}, nil
	}

	return tools.ToolResult{Text: fmt.Sprintf("Failed to send message to Teams. Status: %s", resp.Status)}, nil
}
