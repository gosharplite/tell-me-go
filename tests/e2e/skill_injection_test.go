// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestE2ESkillInjection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	// 1. Setup temp home and mock skill
	homeDir := t.TempDir()
	// Skill injector in NewChatter uses: homeDir/docs/skills
	skillsBaseDir := filepath.Join(homeDir, "docs/skills")
	skillDir := filepath.Join(skillsBaseDir, "golang-patterns")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}

	skillContent := `---
name: golang-patterns
description: Idiomatic Go patterns
---
## Go Patterns
Use idiomatic Go.`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatal(err)
	}

	// 2. Setup mock server to intercept LLM request
	var interceptedRequest string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			return
		}
		body, _ := io.ReadAll(r.Body)
		interceptedRequest = string(body)

		// Return a dummy response to keep the agent happy
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices": [{"message": {"role": "assistant", "content": "I will write a Go function."}}], "usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}}`)
	}))
	defer server.Close()

	// 3. Create config pointing to mock server
	configPath := createTempConfig(t, "openai", server.URL)

	// 4. Run CLI with Go-related prompt
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_NO_STREAM=true",
	}
	
	// We use -new to ensure a clean start and a prompt that matches the skill description/name
	_, stderr, err := runCommandWithEnvInDir(homeDir, env, "", "-c", configPath, "-new", "Write a Go function using patterns")
	if err != nil {
		t.Fatalf("CLI failed: %v\nStderr: %s", err, stderr)
	}

	// 5. Assert skill injection
	if interceptedRequest == "" {
		t.Fatal("No request intercepted by mock server")
	}

	// Verify the request contains the skill injection block
	if !strings.Contains(interceptedRequest, "## Relevant Go Development Skills") {
		t.Errorf("Expected intercepted request to contain '## Relevant Go Development Skills', but it didn't.\nRequest: %s", interceptedRequest)
	}
	if !strings.Contains(interceptedRequest, "golang-patterns") {
		t.Errorf("Expected intercepted request to contain skill name 'golang-patterns', but it didn't.\nRequest: %s", interceptedRequest)
	}
	if !strings.Contains(interceptedRequest, "## Go Patterns") {
		t.Errorf("Expected intercepted request to contain skill content '## Go Patterns', but it didn't.\nRequest: %s", interceptedRequest)
	}
	
	// Also verify it's in a system or developer message
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(interceptedRequest), &req); err != nil {
		t.Fatalf("Failed to unmarshal intercepted request: %v", err)
	}

	found := false
	for _, msg := range req.Messages {
		if (msg.Role == "system" || msg.Role == "developer") && strings.Contains(msg.Content, "## Relevant Go Development Skills") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Skill injection block not found in a system or developer message. Request: %s", interceptedRequest)
	}
}
