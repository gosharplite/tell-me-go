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

func setupSkill(t *testing.T, homeDir, skillName, skillContent string) {
	t.Helper()
	// Skill injector in NewChatter uses: homeDir/docs/skills
	skillsBaseDir := filepath.Join(homeDir, "docs", "skills")
	skillDir := filepath.Join(skillsBaseDir, skillName)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatal(err)
	}
}

func setupMockLLMServer(t *testing.T) (*httptest.Server, <-chan string) {
	t.Helper()
	reqChan := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			return
		}
		body, _ := io.ReadAll(r.Body)

		select {
		case reqChan <- string(body):
		default:
		}

		// Return a dummy response to keep the agent happy
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices": [{"message": {"role": "assistant", "content": "I will write a Go function."}}], "usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}}`)
	}))
	t.Cleanup(server.Close)
	return server, reqChan
}

func validateSkillInjection(t *testing.T, interceptedRequest, skillName, skillContentSnippet string) {
	t.Helper()

	if interceptedRequest == "" {
		t.Fatal("No request intercepted by mock server")
	}

	// Verify the request contains the skill injection block
	assertContains(t, interceptedRequest, "## Relevant Go Development Skills")
	assertContains(t, interceptedRequest, skillName)
	assertContains(t, interceptedRequest, skillContentSnippet)

	// Also verify it's in a system or developer message
	assertSkillInSystemMessage(t, interceptedRequest)
}

func assertContains(t *testing.T, interceptedRequest, expected string) {
	t.Helper()
	if !strings.Contains(interceptedRequest, expected) {
		t.Errorf("Expected intercepted request to contain '%s', but it didn't.\nRequest: %s", expected, interceptedRequest)
	}
}

func assertSkillInSystemMessage(t *testing.T, interceptedRequest string) {
	t.Helper()
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

func TestE2ESkillInjection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	// 1. Setup temp home and mock skill
	homeDir := t.TempDir()
	skillName := "golang-patterns"
	skillContentSnippet := "## Go Patterns"
	skillContent := fmt.Sprintf(`---
name: %s
description: Idiomatic Go patterns
---
%s
Use idiomatic Go.`, skillName, skillContentSnippet)
	setupSkill(t, homeDir, skillName, skillContent)

	// 2. Setup mock server to intercept LLM request
	server, reqChan := setupMockLLMServer(t)
	serverURL := server.URL

	// 3. Create config pointing to mock server
	configPath := createTempConfig(t, "openai", serverURL)

	// 4. Run CLI with Go-related prompt
	env := []string{
		"TELL_ME_HOME=" + homeDir,
	}

	// We use -new to ensure a clean start and a prompt that matches the skill description/name
	_, stderr, err := runCommandWithEnvInDir(homeDir, env, "", "-c", configPath, "--new", "Write a Go function using patterns")
	if err != nil {
		t.Fatalf("CLI failed: %v\nStderr: %s", err, stderr)
	}

	// 5. Assert skill injection
	interceptedRequest := <-reqChan
	validateSkillInjection(t, interceptedRequest, skillName, skillContentSnippet)
}
