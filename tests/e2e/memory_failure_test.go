// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestE2EMemoryWriteFailure_AggregateSurfaces is the primary offline
// write-failure leg (issue #1410 §5 / ADR-068 §8): a full CLI run with
// LEARN: capture and FAKE_PLUR_REJECT=plur_capture proves the WHOLE chain
// through the real StdioClient and the real Chat defer — the fake rejects
// the plur_capture write with isError:true (reject-writes-only; injection
// stays healthy), the hook's domain-outcome detection counts it, and Chat's
// top-of-Chat flush-then-read defer surfaces the session-end aggregate on
// stderr.
//
// A seeded pinned engram proves injection is healthy in the SAME run where
// writes are dead — the exact shipped symptom (issue #1410 §5): the
// intercepted LLM request carries the pinned engram's ## PLUR MEMORY block
// and stderr carries the Info-level memory_injected log.
//
// Surfaces asserted:
//   - memory_injected + e2e-pinned-1 in stderr, plus the pinned engram's
//     id in the intercepted request (injection healthy — Seam A);
//   - memory_write_failures with the exact report "memory write failures: 1;
//     plur_capture failing — learning is disabled" (the dead-tool aggregate
//     from MemoryWriteReport, surfaced by the real Chat defer — Seam B);
//   - zero persistence: capture was rejected, so no episode landed and no
//     engram was minted (only the out-of-band seed exists).
func TestE2EMemoryWriteFailure_AggregateSurfaces(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

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
	configPath := memoryE2EConfig(t, server.URL, fakePlur, "capture")
	env := append(memoryEnv(t, homeDir, server.URL, storePath, seedPath),
		"FAKE_PLUR_REJECT=plur_capture")

	stdout, stderr, err := runMemoryCLI(t, homeDir, env, configPath, "Write a hello world program")
	if err != nil {
		t.Fatalf("CLI failed: %v\nStderr: %s\nStdout: %s", err, stderr, stdout)
	}

	// Injection is healthy in the same run where writes are dead (the
	// shipped symptom): the pinned engram is injected into the LLM request
	// and the Info-level memory_injected log surfaces on stderr.
	intercepted := <-reqChan
	validateMemoryInjection(t, intercepted, "e2e-pinned-1")
	errOut := stripANSI(stderr)
	assertContains(t, errOut, "memory_injected")
	assertContains(t, errOut, "e2e-pinned-1")

	// The write-failure surface: the session-end aggregate from the real
	// Chat defer names the dead tool (exact report format from
	// MemoryWriteReport — note the em dash).
	assertContains(t, errOut, "memory_write_failures")
	assertContains(t, errOut, "memory write failures: 1")
	assertContains(t, errOut, "plur_capture failing — learning is disabled")

	// Zero persistence: capture was rejected, so no episode landed and no
	// engram was minted (only the out-of-band seed exists).
	st := readPlurStore(t, storePath)
	if len(st.Episodes) != 0 {
		t.Errorf("expected zero persisted episodes (capture rejected), got %d", len(st.Episodes))
	}
	for _, e := range st.Engrams {
		if strings.HasPrefix(e.ID, "fakeplur-") {
			t.Errorf("unexpected minted engram %q — writes must not persist", e.ID)
		}
	}
}

// TestE2EMemoryWriteFailure_BatchPartialDrift is the second offline
// write-failure leg (issue #1410 §5): the default-tier partial-drift case.
// With LEARN: batch and FAKE_PLUR_REJECT=plur_learn_batch, the per-turn
// episode is buffered (no MCP call on AfterTurn), and Chat's defer
// FlushSession fires plur_learn_batch at session end — the fake rejects it
// with isError:true, the domain-outcome detection counts the flush's own
// attempt, and the aggregate names plur_learn_batch as dead.
//
// No seed is used: with no engrams the injector logs memory_injected_no_ids
// at Debug (not memory_injected), so this leg stays focused on the
// default-tier write-failure surface.
//
// Surfaces asserted:
//   - memory_write_failures with "memory write failures: 1" naming
//     "plur_learn_batch failing — learning is disabled";
//   - zero persistence: the flush was rejected, so the store has zero
//     engrams and zero episodes.
func TestE2EMemoryWriteFailure_BatchPartialDrift(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	server, reqChan := setupMockLLMServer(t)
	fakePlur := buildFakePlur(t)
	storePath := filepath.Join(t.TempDir(), "plur-store.json")

	homeDir := t.TempDir()
	configPath := memoryE2EConfig(t, server.URL, fakePlur, "batch")
	env := append(memoryEnv(t, homeDir, server.URL, storePath, ""),
		"FAKE_PLUR_REJECT=plur_learn_batch")

	stdout, stderr, err := runMemoryCLI(t, homeDir, env, configPath, "summarize the session notes")
	if err != nil {
		t.Fatalf("CLI failed: %v\nStderr: %s\nStdout: %s", err, stderr, stdout)
	}
	// Drain the intercepted request (proves the run reached the LLM).
	if req := <-reqChan; req == "" {
		t.Fatal("CLI produced no intercepted LLM request")
	}

	// The defer's FlushSession fired plur_learn_batch, which was rejected
	// and counted — the aggregate names the dead tool (exact report format
	// from MemoryWriteReport — note the em dash).
	errOut := stripANSI(stderr)
	assertContains(t, errOut, "memory_write_failures")
	assertContains(t, errOut, "memory write failures: 1")
	assertContains(t, errOut, "plur_learn_batch failing — learning is disabled")

	// Zero persistence: the flush was rejected — nothing landed.
	st := readPlurStore(t, storePath)
	if len(st.Engrams) != 0 {
		t.Errorf("expected zero persisted engrams (batch flush rejected), got %d", len(st.Engrams))
	}
	if len(st.Episodes) != 0 {
		t.Errorf("expected zero persisted episodes (batch flush rejected), got %d", len(st.Episodes))
	}
}
