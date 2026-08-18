// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import "testing"

func TestIsSecretKey(t *testing.T) {
	positives := []string{
		"token",
		"api_key",
		"api_keys",
		"api-key",
		"apikey",
		"auth_token",
		"auth_tokens",
		"authorization",
		"password",
		"passwords",
		"secret",
		"secrets",
		"credential",
		"credentials",
		"username",
		"usernames",
		"key",
		"keys",
		"API_KEY",
		"Api-Key",
		"mcp_servers::atlassian::token",
		"providers::google::apikey",
		"TELL_ME_MCP_SERVERS_ATLASSIAN_TOKEN",
		"github_token",
		"mcp_token",
	}
	negatives := []string{
		"max_tokens",
		"max_history_tokens",
		"token_count",
		"auth",
		"user_id",
		"model",
		"url",
		"context_window",
		"timeout",
		"wrap_width",
	}

	for _, key := range positives {
		if !isSecretKey(key) {
			t.Errorf("isSecretKey(%q) = false; want true", key)
		}
	}
	for _, key := range negatives {
		if isSecretKey(key) {
			t.Errorf("isSecretKey(%q) = true; want false", key)
		}
	}
}

func TestRedactRawContent(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"api key scalar", "API_KEY: sk-123", "API_KEY: [REDACTED]"},
		{"quoted key", `"API_KEY": "sk-123"`, "API_KEY: [REDACTED]"},
		{"space before colon", `API_KEY : "sk-123"`, "API_KEY: [REDACTED]"},
		{"sequence item", "- API_KEY: sk-123", "- API_KEY: [REDACTED]"},
		{"indented key", "  TOKEN: file-token", "  TOKEN: [REDACTED]"},
		{"suppress more-indented continuation", "API_KEY: sk-123\n  continuation line\nMODEL: gpt", "API_KEY: [REDACTED]\nMODEL: gpt"},
		{"suppress equal-indent non-key line", "API_KEY: sk-123\nnot a key line\nMODEL: gpt", "API_KEY: [REDACTED]\nMODEL: gpt"},
		{"colonless secret", "TOKEN sk-123", "TOKEN [REDACTED]"},
		{"colonless secret trailing newline", "TOKEN sk-123\n", "TOKEN [REDACTED]\n"},
		{"empty input", "", ""},
		{"whitespace-only lines", "   \n", "   \n"},
		{"flow sub-key redacted preserving siblings", "HEADERS: {Authorization: Bearer sk-123, X-Other: y}", "HEADERS: {[REDACTED], X-Other: y}"},
		{"brace-less chain redacts to end of line", "MCP_SERVERS: atlassian: TOKEN: file-token", "MCP_SERVERS: [REDACTED]"},
		{"brace-less chain with deny-listed head", "HEADERS: Authorization: Bearer sk-123", "HEADERS: [REDACTED]"},
		{"quoted prose with token passes through", `PERSON: "a token: decline"`, `PERSON: "a token: decline"`},
		{"quoted URL passes through", `URL: "https://example.com/path?token=abc"`, `URL: "https://example.com/path?token=abc"`},
		{"unquoted URL over-redacted", "URL: https://example.com/path?token=abc", "URL: [REDACTED]"},
		{"innocuous-name scalar passes through", "PAYLOAD: sk-1234", "PAYLOAD: sk-1234"},
		{"block scalar key-shaped content fail-closed", "PERSON: |\n  API_KEY: sk-123", "PERSON: |\n  API_KEY: [REDACTED]"},
		{"barred plural tokens passes through", "HEADERS: {tokens: sk-123}", "HEADERS: {tokens: sk-123}"},
		{"multi-line mixed content", "MODE: test\nMODEL: gpt-4\nAPI_KEY: sk-123\n", "MODE: test\nMODEL: gpt-4\nAPI_KEY: [REDACTED]\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redactRawContent(tt.input); got != tt.want {
				t.Errorf("redactRawContent(%q) = %q; want %q", tt.input, got, tt.want)
			}
		})
	}
}
