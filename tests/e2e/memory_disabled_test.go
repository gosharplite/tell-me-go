// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2EMemoryDisabled_ZeroPersistence is the master-switch leg (issue
// #1414): with MEMORY.ENABLED=false and a non-off LEARN tier, a full CLI run
// must persist NOTHING to the PLUR store — the injector strips on disable and
// the hook returns before tier dispatch, so the fake's store file gains zero
// engrams and zero episodes. The store file itself must exist (the fake's
// startup persist ran — proving the MCP server was actually spawned and saw
// zero writes), and stderr must carry none of the memory write/injection
// surfaces.
func TestE2EMemoryDisabled_ZeroPersistence(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	for _, tier := range []string{"capture", "batch"} {
		t.Run(tier, func(t *testing.T) {
			server, reqChan := setupMockLLMServer(t)
			fakePlur := buildFakePlur(t)
			storePath := filepath.Join(t.TempDir(), "plur-store.json")

			homeDir := t.TempDir()
			configPath := memoryE2EConfigWithEnabled(t, server.URL, fakePlur, tier, false)
			env := memoryEnv(t, homeDir, server.URL, storePath, "")

			_, stderr, err := runMemoryCLI(t, homeDir, env, configPath, "Write a hello world program")
			if err != nil {
				t.Fatalf("CLI failed: %v\nStderr: %s", err, stderr)
			}
			// Drain the intercepted request (proves the run reached the LLM).
			if req := <-reqChan; req == "" {
				t.Fatal("CLI produced no intercepted LLM request")
			}

			// The store file must exist — the fake's startup persist ran —
			// and must contain zero engrams and zero episodes.
			if _, err := os.Stat(storePath); err != nil {
				t.Fatalf("fake plur store file missing after CLI run: %v", err)
			}
			st := readPlurStore(t, storePath)
			if len(st.Engrams) != 0 {
				t.Errorf("expected zero persisted engrams with ENABLED=false (tier %s), got %d", tier, len(st.Engrams))
			}
			if len(st.Episodes) != 0 {
				t.Errorf("expected zero persisted episodes with ENABLED=false (tier %s), got %d", tier, len(st.Episodes))
			}

			// No memory surfaces on stderr: no write-failure report, no
			// injection log, no per-write failure Warns.
			errOut := stripANSI(stderr)
			for _, forbidden := range []string{
				"memory_write_failures",
				"memory_injected",
				"memory_injection_failed",
				"memory_capture_failed",
				"memory_learn_failed",
				"memory_learn_batch_failed",
			} {
				if strings.Contains(errOut, forbidden) {
					t.Errorf("stderr must not contain %q with ENABLED=false (tier %s); stderr:\n%s", forbidden, tier, errOut)
				}
			}
		})
	}
}
