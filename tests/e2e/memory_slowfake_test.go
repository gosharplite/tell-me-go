// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

// TestE2EMemoryBatchSlowFake_SymptomReproduced is the offline E2E leg that
// reproduces the shipped symptom of issue #1412 through the real CLI, the
// real StdioClient, and the fake PLUR server: with LEARN: batch, the
// session-end plur_learn_batch flush (Chat's defer FlushSession) is made
// against a server leg that sleeps longer than the hook's 3s detached
// timeout (detachedTimeout, ADR-068 §2.2). FAKE_PLUR_DELAY_MS_FILE=4000 +
// FAKE_PLUR_DELAY_TOOL=plur_learn_batch scope the artificial delay to the
// batch flush only, so injection stays healthy and the run itself completes
// normally — the turn finishes, the CLI exits, and only the deferred flush
// observes the slow leg.
//
// Surfaces asserted (the exact shipped issue #1412 log lines):
//   - memory_learn_batch_failed (the hook's failure Warn, carrying the
//     detached-context deadline error);
//   - "context deadline exceeded" (the StdioClient surfaces the 3s detached
//     context expiry as a wrapped context.DeadlineExceeded — the child is
//     still alive, so the deadline error is NOT replaced by a child-exit
//     error);
//   - memory_write_failures with "memory write failures: 1" naming
//     "plur_learn_batch failing — learning is disabled" (the session-end
//     aggregate from MemoryWriteReport, surfaced by the real Chat defer);
//   - zero persistence: the fake's delayed call returns isError WITHOUT
//     processing (T1 contract), so the store holds zero engrams and zero
//     episodes — a call the client already abandoned never mutates the
//     store.
//
// The retained-buffer retry proof (the claim survives a failed flush and is
// restored for the next flush opportunity) is the sibling leg
// TestE2EMemoryBatchRetainedAcrossFailedFlush_RealStdioClient (next task);
// this leg pins only the shipped symptom + zero-persistence contract.
func TestE2EMemoryBatchSlowFake_SymptomReproduced(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	server, reqChan := setupMockLLMServer(t)
	fakePlur := buildFakePlur(t)
	storePath := filepath.Join(t.TempDir(), "plur-store.json")
	delayPath := filepath.Join(t.TempDir(), "plur-delay-ms")
	if err := os.WriteFile(delayPath, []byte("4000"), 0644); err != nil {
		t.Fatalf("write delay file: %v", err)
	}

	homeDir := t.TempDir()
	configPath := memoryE2EConfig(t, server.URL, fakePlur, "batch")
	env := append(memoryEnv(t, homeDir, server.URL, storePath, ""),
		"FAKE_PLUR_DELAY_MS_FILE="+delayPath,
		"FAKE_PLUR_DELAY_TOOL=plur_learn_batch")

	stdout, stderr, err := runMemoryCLI(t, homeDir, env, configPath, "summarize the session notes")
	if err != nil {
		t.Fatalf("CLI failed: %v\nStderr: %s\nStdout: %s", err, stderr, stdout)
	}

	// Drain the intercepted request (proves the run reached the LLM — the
	// turn completes before the session-end flush).
	if req := <-reqChan; req == "" {
		t.Fatal("CLI produced no intercepted LLM request")
	}

	// The defer's FlushSession fired plur_learn_batch against the 4s-delayed
	// leg; the 3s detached context expired, so the failure Warn names the
	// deadline error and the session-end aggregate names the dead tool
	// (exact report format from MemoryWriteReport — note the em dash).
	errOut := stripANSI(stderr)
	assertContains(t, errOut, "memory_learn_batch_failed")
	assertContains(t, errOut, "context deadline exceeded")
	assertContains(t, errOut, "memory_write_failures")
	assertContains(t, errOut, "memory write failures: 1")
	assertContains(t, errOut, "plur_learn_batch failing — learning is disabled")

	// Zero persistence: the delayed call failed without processing — nothing
	// landed (T1 contract).
	st := readPlurStore(t, storePath)
	if len(st.Engrams) != 0 {
		t.Errorf("expected zero persisted engrams (batch flush timed out), got %d", len(st.Engrams))
	}
	if len(st.Episodes) != 0 {
		t.Errorf("expected zero persisted episodes (batch flush timed out), got %d", len(st.Episodes))
	}
}
