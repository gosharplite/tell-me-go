//go:build e2e_live

// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Live legs for the PLUR memory integration (ADR-068 §8): these tests are
// compiled only under -tags=e2e_live and are skipped by default — they are
// never part of make check-full, which must stay 100% offline.
//
// Environment verification procedure:
//
//	go test -tags=e2e_live ./tests/e2e/ -run TestLivePlur -v
//
// Prerequisites: Node/npx + network access, and a MEMORY.SERVER: plur
// config pointing at a real `npx -y @plur-ai/mcp` server. Seed a pinned
// engram out-of-band via the PLUR CLI (`plur learn` / store edit) — the
// documented no-pin-tool step (ADR-068 §13) — then verify it is injected.
//
// Posture: best-effort, log-and-pass — never a hard gate. Either
// memory_injected (Info) or memory_injection_failed (Warn) in stderr proves
// the injector reached the real server and the handshake happened.
package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// liveMemoryE2EConfig writes a config identical to memoryE2EConfig except
// that MCP_SERVERS.plur launches the real `npx -y @plur-ai/mcp` server.
func liveMemoryE2EConfig(t *testing.T, mockURL string) string {
	t.Helper()
	content := fmt.Sprintf(`
MODE: "assistant"
SELECTED_PROVIDER: "mock"
PROVIDERS:
  mock:
    TYPE: "openai"
    URL: "%s"
    API_KEY: "dummy"
    MODEL: "gpt-4"
MCP_SERVERS:
  plur:
    COMMAND: "npx"
    ARGS: ["-y", "@plur-ai/mcp"]
MEMORY:
  ENABLED: true
  SERVER: "plur"
  INJECT_BUDGET: 2000
  LEARN: "off"
`, mockURL)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write live memory config: %v", err)
	}
	return path
}

// TestLivePlurMemoryInjection verifies the real PLUR handshake against a
// live npx-spawned server. Best-effort: any outcome (run failure, absent
// surface, or observed handshake) is logged and passed — this test never
// gates the build.
func TestLivePlurMemoryInjection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow live E2E test in short mode")
	}

	server, _ := setupMockLLMServer(t)
	homeDir := t.TempDir()
	configPath := liveMemoryE2EConfig(t, server.URL)
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"HOME=" + filepath.Join(t.TempDir(), "home"),
		"TELL_ME_DEBUG=1", // surface the Info-level memory_injected log
		"TELL_ME_MOCK_URL=" + server.URL,
	}

	_, stderr, err := runMemoryCLI(t, homeDir, env, configPath, "Write a hello world program")
	if err != nil {
		t.Logf("live PLUR run did not complete (npx/network unavailable?): %v\nStderr: %s", err, stripANSI(stderr))
		return // log-and-pass — never a hard gate
	}

	errOut := stripANSI(stderr)
	if strings.Contains(errOut, "memory_injected") || strings.Contains(errOut, "memory_injection_failed") {
		t.Logf("live PLUR handshake verified: the injector reached the real npx server (memory_injected / memory_injection_failed observed)")
		return
	}
	t.Logf("live PLUR run completed but no injector surface was observed in stderr; check the environment procedure (seeded pinned engram + MEMORY.SERVER: plur). Stderr:\n%s", errOut)
}
