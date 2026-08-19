// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestE2EMemoryLearning_TeachOneInjectAnother is Leg (ii)(a) of the
// two-legged PLUR memory E2E (ADR-068 §8 / issue #1404): a gated direct
// learn in one persona lands in the shared store, and a second persona with
// a FRESH home but the SAME store receives it via plur_inject_hybrid.
//
// Persona A (LEARN: full) teaches with a "please remember" correction frame
// → the hook's gated plur_learn fires and persists a fakeplur-<n> engram.
// Persona B (LEARN: full, fresh home, same FAKE_PLUR_STORE) asks a
// semantically adjacent question (no correction frame → no new learn) and
// receives the learned engram in its context. The subtests sharing one
// store file run sequentially — never t.Parallel.
func TestE2EMemoryLearning_TeachOneInjectAnother(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	serverA, reqChanA := setupMockLLMServer(t)
	fakePlur := buildFakePlur(t)
	storePath := filepath.Join(t.TempDir(), "plur-store.json")
	statement := "please remember: always use the LEGACY_X approach for this codebase"

	// Persona A teaches into the shared store.
	homeA := t.TempDir()
	configA := memoryE2EConfig(t, serverA.URL, fakePlur, "full")
	envA := memoryEnv(t, homeA, serverA.URL, storePath, "")
	_, stderrA, err := runMemoryCLI(t, homeA, envA, configA, statement)
	if err != nil {
		t.Fatalf("persona A CLI failed: %v\nStderr: %s", err, stderrA)
	}
	// Drain persona A's intercepted request (proves the run reached the LLM).
	if req := <-reqChanA; req == "" {
		t.Fatal("persona A produced no intercepted LLM request")
	}

	// The gated learn landed in the shared store.
	st := readPlurStore(t, storePath)
	learnedID, ok := findEngramByText(st, statement)
	if !ok {
		t.Fatalf("statement %q not found in shared store; store: %+v", statement, st)
	}
	if !strings.HasPrefix(learnedID, "fakeplur-") {
		t.Errorf("learned id %q does not match the deterministic fakeplur-<n> shape", learnedID)
	}

	// Persona B: fresh home, same store; semantically adjacent prompt with
	// no correction frame (no new learn).
	serverB, reqChanB := setupMockLLMServer(t)
	homeB := t.TempDir()
	configB := memoryE2EConfig(t, serverB.URL, fakePlur, "full")
	envB := memoryEnv(t, homeB, serverB.URL, storePath, "")
	_, stderrB, err := runMemoryCLI(t, homeB, envB, configB, "what should I know about LEGACY_X?")
	if err != nil {
		t.Fatalf("persona B CLI failed: %v\nStderr: %s", err, stderrB)
	}

	intercepted := <-reqChanB
	validateMemoryInjection(t, intercepted, learnedID)

	errOutB := stripANSI(stderrB)
	assertContains(t, errOutB, "memory_injected")
	assertContains(t, errOutB, learnedID)
}

// TestE2EMemoryLearning_BatchFlushAtSessionEnd is Leg (ii)(a2): it proves
// the session-end plur_learn_batch flush end-to-end. With LEARN: batch and
// a plain prompt (no correction frame), the hook buffers the turn's episode
// and Chat's `defer FlushSession` (internal/agent/agent.go) drains the ring
// buffer on the success path — the plur_learn_batch payload lands in the
// store's episodes section before the CLI process exits.
func TestE2EMemoryLearning_BatchFlushAtSessionEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	server, reqChan := setupMockLLMServer(t)
	fakePlur := buildFakePlur(t)
	storePath := filepath.Join(t.TempDir(), "plur-store.json")

	homeDir := t.TempDir()
	configPath := memoryE2EConfig(t, server.URL, fakePlur, "batch")
	env := memoryEnv(t, homeDir, server.URL, storePath, "")

	_, stderr, err := runMemoryCLI(t, homeDir, env, configPath, "summarize the session notes")
	if err != nil {
		t.Fatalf("CLI failed: %v\nStderr: %s", err, stderr)
	}
	// Drain the intercepted request (proves the run reached the LLM).
	if req := <-reqChan; req == "" {
		t.Fatal("CLI produced no intercepted LLM request")
	}

	// The defer FlushSession fired on the success path: the store's episodes
	// section is non-empty and contains the model response text (the mock
	// always answers "I will write a Go function.").
	st := readPlurStore(t, storePath)
	if len(st.Episodes) == 0 {
		t.Fatal("expected batch episodes in the store after session end; plur_learn_batch did not land")
	}
	if texts := episodeTexts(st); !strings.Contains(texts, "I will write a Go function.") {
		t.Errorf("stored episodes do not contain the model response text; episodes: %q", texts)
	}
}

// TestE2EMemoryLearning_BestEffortSemanticAdjacency is Leg (ii)(b): a
// semantically-adjacent-but-not-self-revealing prompt in persona B surfaces
// the learned engram. The fake's selection is a deterministic case-insensitive
// keyword substring gate, so the match is crafted to be deterministic: the
// learned statement's keywords include "gizmo" and persona B's prompt shares
// that keyword ("is the GIZMO flag needed?") without repeating the full
// instruction.
//
// Best-effort posture per the issue's acceptance criteria: this leg is
// explicitly NON-GATING. On a miss the test logs the reason and re-runs
// persona B ONCE (the documented re-run allowance); if the re-run also
// misses it logs that the allowance is exhausted and PASSES — it never
// calls t.Fail for a missed injection. The stderr assertion (the
// memory_injected Info log carrying the learned id) runs only after a
// successful injection.
func TestE2EMemoryLearning_BestEffortSemanticAdjacency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	serverA, reqChanA := setupMockLLMServer(t)
	fakePlur := buildFakePlur(t)
	storePath := filepath.Join(t.TempDir(), "plur-store.json")
	statement := "please remember: the GIZMO flag must be set for cache-busting"

	// Persona A teaches.
	homeA := t.TempDir()
	configA := memoryE2EConfig(t, serverA.URL, fakePlur, "full")
	envA := memoryEnv(t, homeA, serverA.URL, storePath, "")
	_, stderrA, err := runMemoryCLI(t, homeA, envA, configA, statement)
	if err != nil {
		t.Fatalf("persona A CLI failed: %v\nStderr: %s", err, stderrA)
	}
	if req := <-reqChanA; req == "" {
		t.Fatal("persona A produced no intercepted LLM request")
	}
	st := readPlurStore(t, storePath)
	learnedID, ok := findEngramByText(st, statement)
	if !ok {
		t.Fatalf("statement %q not learned into the shared store; store: %+v", statement, st)
	}

	// Persona B: adjacent prompt sharing the "GIZMO" keyword.
	runPersonaB := func() string {
		serverB, reqChanB := setupMockLLMServer(t)
		homeB := t.TempDir()
		configB := memoryE2EConfig(t, serverB.URL, fakePlur, "full")
		envB := memoryEnv(t, homeB, serverB.URL, storePath, "")
		_, stderrB, err := runMemoryCLI(t, homeB, envB, configB, "is the GIZMO flag needed?")
		if err != nil {
			t.Fatalf("persona B CLI failed: %v\nStderr: %s", err, stderrB)
		}
		intercepted := <-reqChanB
		if !strings.Contains(intercepted, "## PLUR MEMORY") || !strings.Contains(intercepted, learnedID) {
			t.Logf("persona B run missed injection of %q (intercepted request lacks the learned id)", learnedID)
			return ""
		}
		return stripANSI(stderrB)
	}

	errOutB := runPersonaB()
	if errOutB == "" {
		// Documented re-run allowance: exactly one re-run, then log-and-pass.
		t.Logf("best-effort leg (ii)(b) missed on the first persona B run (learned id %q); exercising the documented re-run allowance", learnedID)
		errOutB = runPersonaB()
	}
	if errOutB == "" {
		t.Logf("best-effort leg did not converge; documented re-run allowance exhausted (learned id %q never injected) — passing per acceptance criteria", learnedID)
		return
	}

	assertContains(t, errOutB, "memory_injected")
	assertContains(t, errOutB, learnedID)
}
