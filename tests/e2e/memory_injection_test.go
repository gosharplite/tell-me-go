// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

// TestE2EMemoryInjection_PinnedEngramInjected is Leg (i) of the two-legged
// PLUR memory E2E (ADR-068 §8 / issue #1404): a pinned engram seeded
// out-of-band must be injected into a fresh persona's context on an
// UNRELATED task — proving pinned engrams bypass the fake server's
// relevance gate and that the whole plumbing (config → DI → stdio spawn →
// plur_inject_hybrid → context transformer → system prompt) works offline
// end-to-end. LEARN is off, so no learning happens in this leg.
//
// Surfaces asserted (ADR-068 §8 observability):
//   - the intercepted LLM request carries the ## PLUR MEMORY block with the
//     pinned engram id (in-process/telemetry surface);
//   - stderr carries the Info-level memory_injected log (black-box surface;
//     TELL_ME_DEBUG=1 in memoryEnv lowers the CLI's default Warn level).
func TestE2EMemoryInjection_PinnedEngramInjected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}
	t.Parallel()

	server, reqChan := setupMockLLMServer(t)
	fakePlur := buildFakePlur(t)

	storePath := filepath.Join(t.TempDir(), "plur-store.json")
	seedPath := writeSeedFile(t, filepath.Join(t.TempDir(), "plur-seed.json"),
		[]map[string]interface{}{
			{
				"id":       "e2e-pinned-1",
				"text":     "Always use the E2E_MEMORY convention in test code.",
				"keywords": []interface{}{"unrelated-keyword"},
				"pinned":   true,
			},
		})

	homeDir := t.TempDir()
	configPath := memoryE2EConfig(t, server.URL, fakePlur, "off")
	env := memoryEnv(t, homeDir, server.URL, storePath, seedPath)

	// The prompt is deliberately unrelated to the seeded engram: only the
	// pinned flag can select it.
	stdout, stderr, err := runMemoryCLI(t, homeDir, env, configPath, "Write a hello world program")
	if err != nil {
		t.Fatalf("CLI failed: %v\nStderr: %s\nStdout: %s", err, stderr, stdout)
	}

	// In-process/telemetry surface: the mock LLM request carries the memory
	// block with the pinned engram id.
	interceptedRequest := <-reqChan
	validateMemoryInjection(t, interceptedRequest, "e2e-pinned-1")

	// Black-box surface: the Info-level memory_injected log renders on
	// stderr with the injected engram id (slog text handler: msg=memory_injected
	// engrams=e2e-pinned-1 — the in-process warning string is
	// injected_engrams:e2e-pinned-1, ADR-068 §8).
	errOut := stripANSI(stderr)
	assertContains(t, errOut, "memory_injected")
	assertContains(t, errOut, "e2e-pinned-1")

	// The fake ran: it created the store file at startup even though LEARN
	// is off (no mutations in this leg).
	if _, statErr := os.Stat(storePath); statErr != nil {
		t.Fatalf("fake plur store file %s was not created (fake did not run): %v", storePath, statErr)
	}
}
