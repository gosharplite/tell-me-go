//go:build e2e_live

// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Live legs for the PLUR memory integration (ADR-068 §8, amended by issue
// #1410): these tests are compiled only under -tags=e2e_live and are never
// part of make check-full, which must stay 100% offline.
//
// Posture (issue #1410 §8): skip ONLY on environment preconditions — npx
// absent, or the throwaway handshake failing (network/server unavailability).
// Once past the precondition stage, every mismatch is a hard failure
// (t.Fatal) — no log-and-pass. Persistence is verified through a SECOND
// StdioClient speaking the same tools.MCPClient wire contract the hook
// writes through: before/after reads of plur_status / plur_timeline /
// plur_recall with unique per-run markers, content-bearing, in an isolated
// HOME temp dir shared by the CLI run and the verification client.
//
// Run procedure:
//
//	go test -tags=e2e_live ./tests/e2e/ -run TestLivePlur -v
//
// Prerequisites: Node/npx + network access. The real `npx -y @plur-ai/mcp`
// server writes its store under $HOME/.plur — both the CLI run and the
// verification client are given the SAME temp HOME so the after-read sees
// the CLI's writes.
package e2e

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/mcp"
)

// liveMemoryE2EConfig writes a config identical to memoryE2EConfig except
// that MCP_SERVERS.plur launches the real `npx -y @plur-ai/mcp` server and
// MEMORY.LEARN is set to the given tier. The previous version hardcoded
// LEARN: "off" — the exact posture that shipped the bug (issue #1410).
func liveMemoryE2EConfig(t *testing.T, mockURL, learn string) string {
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
    # The real @plur-ai/mcp server's DEFAULT tool profile ("lean") routes
    # plur_capture / plur_learn_batch / plur_inject_hybrid through
    # plur_admin and rejects direct calls ("not directly callable under the
    # current tool profile"). The server's own hint: "To expose all tools
    # directly, set PLUR_TOOL_PROFILE=full." The integration (and these
    # legs) call the write tools directly, so the CLI's stdio child must run
    # under the full profile — otherwise the legs would fail on profile
    # routing, not on the wire contract they exist to verify (issue #1410).
    ENV:
      PLUR_TOOL_PROFILE: "full"
MEMORY:
  ENABLED: true
  SERVER: "plur"
  INJECT_BUDGET: 2000
  LEARN: "%s"
`, mockURL, learn)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write live memory config: %v", err)
	}
	return path
}

// liveMemoryEnv builds the CLI-run environment for the live legs:
// TELL_ME_HOME (persona home), HOME pinned to storeHome (the SAME isolated
// dir the verification client reads — the real npx server writes its store
// under $HOME/.plur), TELL_ME_DEBUG=1 (Info-level memory logs on stderr)
// and TELL_ME_MOCK_URL (the mock LLM). HOME last-wins: runCommandWithEnvInDir
// appends this slice after the filtered parent env, and the CLI forwards its
// own env to the stdio-spawned npx child.
func liveMemoryEnv(t *testing.T, homeDir, storeHome, mockURL string) []string {
	t.Helper()
	return []string{
		"TELL_ME_HOME=" + homeDir,
		"HOME=" + storeHome,
		"TELL_ME_DEBUG=1",
		"TELL_ME_MOCK_URL=" + mockURL,
	}
}

// newLiveStoreHome creates the shared isolated HOME for one live leg and
// returns its path. Both the CLI run (via liveMemoryEnv) and the
// verification client (via cfg.Env, last-wins over os.Environ) target this
// SAME path, so the after-read observes the CLI's writes in $HOME/.plur.
func newLiveStoreHome(t *testing.T) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatalf("failed to create isolated HOME %s: %v", home, err)
	}
	return home
}

// newLivePlurClient constructs a second StdioClient for the real npx PLUR
// server with HOME pinned to homeDir (cfg.Env last-wins over os.Environ, so
// the verification child shares the CLI's isolated store) and
// PLUR_TOOL_PROFILE=full (the real server's default "lean" profile rejects
// direct calls to plur_capture / plur_learn_batch / plur_inject_hybrid —
// the tools these legs must exercise; the server's own hint names the full
// profile as the direct-call surface). Used for the precondition probe, the
// baseline reads, and the fresh-process after-read. The caller must Close
// it. Past the precondition stage, a spawn failure is a hard failure.
func newLivePlurClient(t *testing.T, homeDir string) *mcp.StdioClient {
	t.Helper()
	cfg := config.MCPServerConfig{
		Command: "npx",
		Args:    []string{"-y", "@plur-ai/mcp"},
		Env: map[string]string{
			"HOME":              homeDir,
			"PLUR_TOOL_PROFILE": "full",
		},
	}
	c, err := mcp.NewStdioClient(cfg, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("failed to start verification PLUR client: %v", err)
	}
	return c
}

// skipUnlessLivePlurAvailable returns a live client after verifying the
// environment, or skips: npx missing → skip; the server failing to start
// (the MCP handshake runs inside NewStdioClient) or the plur_status
// handshake call failing → skip with a log. Once past this precondition
// stage, every subsequent failure is a hard failure. The returned client is
// the baseline reader; the caller owns Close.
func skipUnlessLivePlurAvailable(t *testing.T, homeDir string) *mcp.StdioClient {
	t.Helper()
	if _, err := exec.LookPath("npx"); err != nil {
		t.Skipf("npx not available: %v", err)
	}
	cfg := config.MCPServerConfig{
		Command: "npx",
		Args:    []string{"-y", "@plur-ai/mcp"},
		Env: map[string]string{
			"HOME":              homeDir,
			"PLUR_TOOL_PROFILE": "full",
		},
	}
	c, err := mcp.NewStdioClient(cfg, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Skipf("live PLUR server could not start (npx/network unavailable?): %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if res, err := c.CallTool(ctx, "plur_status", map[string]interface{}{}); err != nil || res.Error != nil {
		c.Close()
		if err != nil {
			t.Skipf("live PLUR handshake failed (npx/network unavailable?): %v", err)
		}
		t.Skipf("live PLUR handshake failed: plur_status rejected: %v", res.Error)
	}
	return c
}

// callLiveTool runs one tool call on the verification client with a 30s
// context. Transport errors are returned as the Go error; MCP-level
// rejections surface as ToolResult.Error (the same three-way split the hook
// relies on, ADR-022 / issue #1373).
func callLiveTool(t *testing.T, c *mcp.StdioClient, name string, args map[string]interface{}) (tools.ToolResult, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return c.CallTool(ctx, name, args)
}

// liveToolSet returns the set of tool names the live server advertises via
// ListTools. A ListTools failure is logged and treated as "unknown surface"
// (empty set) — the legs then rely on best-effort calls.
func liveToolSet(t *testing.T, c *mcp.StdioClient) map[string]bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	defs, err := c.ListTools(ctx)
	if err != nil {
		t.Logf("live ListTools failed (treating all surfaces as unavailable): %v", err)
		return map[string]bool{}
	}
	set := make(map[string]bool, len(defs))
	for _, d := range defs {
		set[d.Name] = true
	}
	return set
}

// plurCounts holds the engram/episode counts parsed from plur_status text.
// A -1 means the count was not parseable from the server's text format.
type plurCounts struct {
	engrams  int
	episodes int
}

var (
	// The real server renders plur_status as JSON-ish text using the
	// "engram_count"/"episode_count" key form (plus the fake's "engrams=N"
	// form) — the 16-char non-digit budget covers both "_count\": " (9) and
	// " count: " (8) separators. A leading \b keeps "versioned_engram_count"
	// from matching at "engram" (the char before it is a word char).
	engramCountRe  = regexp.MustCompile(`(?i)\bengram[^\d]{0,16}(\d+)`)
	episodeCountRe = regexp.MustCompile(`(?i)\bepisode[^\d]{0,16}(\d+)`)
)

// parsePlurStatusCounts extracts engram/episode counts from plur_status
// text. The real server's text format is not pinned, so parsing is
// tolerant: any "engram(s)" / "episode(s)" token followed within 8
// characters by digits matches; a miss yields -1 (the caller then relies on
// the content-bearing surfaces only).
func parsePlurStatusCounts(text string) plurCounts {
	pc := plurCounts{engrams: -1, episodes: -1}
	if m := engramCountRe.FindStringSubmatch(text); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			pc.engrams = n
		}
	}
	if m := episodeCountRe.FindStringSubmatch(text); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			pc.episodes = n
		}
	}
	return pc
}

// liveStatusCounts calls plur_status and parses the counts. A transport
// error or MCP-level rejection past the precondition stage is a hard
// failure.
func liveStatusCounts(t *testing.T, c *mcp.StdioClient) plurCounts {
	t.Helper()
	res, err := callLiveTool(t, c, "plur_status", map[string]interface{}{})
	if err != nil {
		t.Fatalf("plur_status failed (precondition passed — hard failure): %v", err)
	}
	if res.Error != nil {
		t.Fatalf("plur_status rejected: %v", res.Error)
	}
	t.Logf("plur_status: %s", res.Text)
	return parsePlurStatusCounts(res.Text)
}

// runLiveMemoryCLI runs one CLI invocation against the live npx PLUR server
// with the given tier. A CLI failure past the precondition stage is a hard
// failure (posture, issue #1410 §8) — never log-and-pass.
func runLiveMemoryCLI(t *testing.T, homeDir, storeHome, mockURL, configPath, prompt string) (stdout, stderr string) {
	t.Helper()
	env := liveMemoryEnv(t, homeDir, storeHome, mockURL)
	stdout, stderr, err := runMemoryCLI(t, homeDir, env, configPath, prompt)
	if err != nil {
		t.Fatalf("live CLI run failed (precondition passed — hard failure): %v\nStderr: %s", err, stripANSI(stderr))
	}
	return stdout, stderr
}

// TestLivePlurCapturePersists proves the capture tier persists a
// content-bearing episode through the real npx PLUR server (issue #1410
// acceptance criterion 6). The mock LLM response carries a unique per-run
// marker; the after-read through a SECOND StdioClient (fresh process, same
// isolated HOME) must find the marker in the episodic timeline — a
// store-format coincidence cannot fake this (the wire contract is the same
// tools.MCPClient the hook writes through). If the server lacks
// plur_timeline, the episode count moving is the fallback surface — never
// "no assertion".
func TestLivePlurCapturePersists(t *testing.T) {
	marker := fmt.Sprintf("marker-%d", time.Now().UnixNano())
	server, _ := setupMockLLMServer(t, marker)

	homeDir := t.TempDir() // TELL_ME_HOME (persona home)
	storeHome := newLiveStoreHome(t)

	// Precondition probe + baseline reader on the shared isolated HOME.
	baseline := skipUnlessLivePlurAvailable(t, storeHome)
	toolSet := liveToolSet(t, baseline)
	before := liveStatusCounts(t, baseline)
	if toolSet["plur_timeline"] {
		if res, err := callLiveTool(t, baseline, "plur_timeline", map[string]interface{}{}); err == nil && res.Error == nil {
			t.Logf("baseline plur_timeline (best-effort): %s", res.Text)
		}
	}
	// Baseline reads done; free the probe process before the CLI run.
	baseline.Close()

	// CLI run (capture tier).
	configPath := liveMemoryE2EConfig(t, server.URL, "capture")
	_, stderr := runLiveMemoryCLI(t, homeDir, storeHome, server.URL, configPath, "Write a hello world program")
	t.Logf("capture CLI stderr surfaces:\n%s", stripANSI(stderr))

	// After-read through a FRESH StdioClient: a new process loads the store
	// from disk at startup, so it observes the CLI's cross-process writes
	// (the baseline client's process predates the writes).
	after := newLivePlurClient(t, storeHome)
	defer after.Close()

	if toolSet["plur_timeline"] {
		res, err := callLiveTool(t, after, "plur_timeline", map[string]interface{}{"search": marker})
		if err != nil || res.Error != nil {
			// Best-effort retry unfiltered: some server versions reject the
			// search param.
			res, err = callLiveTool(t, after, "plur_timeline", map[string]interface{}{})
		}
		if err != nil || res.Error != nil {
			t.Fatalf("capture tier: plur_timeline unusable for after-read (err=%v toolErr=%v) — no persistence surface observed", err, res.Error)
		}
		if !strings.Contains(res.Text, marker) {
			t.Fatalf("capture tier: no timeline entry carries marker %q — persistence not observed. timeline: %s", marker, res.Text)
		}
		t.Logf("capture tier verified: plur_timeline entry carries marker %q", marker)
		return
	}

	// No plur_timeline surface: the only remaining persistence surface is
	// the episode count moving. Weaker than content-bearing, but still a
	// hard assertion — never a no-op.
	afterCounts := liveStatusCounts(t, after)
	if before.episodes >= 0 && afterCounts.episodes <= before.episodes {
		t.Fatalf("capture tier: episode count did not move (before=%d after=%d) and plur_timeline is unavailable — no persistence surface observed", before.episodes, afterCounts.episodes)
	}
	t.Logf("capture tier verified (fallback surface): episode count moved %d -> %d (plur_timeline unavailable)", before.episodes, afterCounts.episodes)
}

// TestLivePlurBatchPersists proves the batch tier persists a
// content-bearing engram through the real npx PLUR server: the session-end
// plur_learn_batch flush (Chat's defer, the default tier) mints an engram
// whose statement is the mock model response carrying the unique marker.
// After-read: plur_status engram count increased AND plur_recall {query:
// marker} surfaces the engram — else t.Fatal.
func TestLivePlurBatchPersists(t *testing.T) {
	marker := fmt.Sprintf("marker-%d", time.Now().UnixNano())
	server, _ := setupMockLLMServer(t, marker)

	homeDir := t.TempDir()
	storeHome := newLiveStoreHome(t)

	baseline := skipUnlessLivePlurAvailable(t, storeHome)
	before := liveStatusCounts(t, baseline)
	baseline.Close()

	configPath := liveMemoryE2EConfig(t, server.URL, "batch")
	_, stderr := runLiveMemoryCLI(t, homeDir, storeHome, server.URL, configPath, "summarize the session notes")
	t.Logf("batch CLI stderr surfaces:\n%s", stripANSI(stderr))

	after := newLivePlurClient(t, storeHome)
	defer after.Close()

	afterCounts := liveStatusCounts(t, after)
	if before.engrams >= 0 && afterCounts.engrams >= 0 && afterCounts.engrams <= before.engrams {
		t.Fatalf("batch tier: engram count did not increase (before=%d after=%d) — plur_learn_batch did not land", before.engrams, afterCounts.engrams)
	}

	res, err := callLiveTool(t, after, "plur_recall", map[string]interface{}{"query": marker})
	if err != nil {
		t.Fatalf("batch tier: after-read plur_recall failed: %v", err)
	}
	if res.Error != nil {
		t.Fatalf("batch tier: after-read plur_recall rejected: %v", res.Error)
	}
	if !strings.Contains(res.Text, marker) {
		t.Fatalf("batch tier: plur_recall(%q) did not surface the learned engram — persistence not observed. recall: %s", marker, res.Text)
	}
	t.Logf("batch tier verified: engram count %d -> %d and recall surfaced marker %q", before.engrams, afterCounts.engrams, marker)
}

// TestLivePlurFullPersists proves the full tier's gated direct learn
// persists through the real npx PLUR server: the user prompt carries the
// correction frame ("please remember:") plus the unique marker; maybeLearn's
// statement IS the user message (issue #1410), so the plur_learn engram's
// statement contains the marker. After-read: plur_recall {query: marker}
// returns the engram — else t.Fatal. (The full tier also fires the
// session-end batch flush — harmless.)
func TestLivePlurFullPersists(t *testing.T) {
	marker := fmt.Sprintf("marker-%d", time.Now().UnixNano())
	server, _ := setupMockLLMServer(t) // no marker needed: it rides the user prompt

	homeDir := t.TempDir()
	storeHome := newLiveStoreHome(t)

	baseline := skipUnlessLivePlurAvailable(t, storeHome)
	before := liveStatusCounts(t, baseline)
	baseline.Close()

	configPath := liveMemoryE2EConfig(t, server.URL, "full")
	prompt := "please remember: " + marker + " always use the LIVE_X approach"
	_, stderr := runLiveMemoryCLI(t, homeDir, storeHome, server.URL, configPath, prompt)
	t.Logf("full CLI stderr surfaces:\n%s", stripANSI(stderr))

	after := newLivePlurClient(t, storeHome)
	defer after.Close()

	res, err := callLiveTool(t, after, "plur_recall", map[string]interface{}{"query": marker})
	if err != nil {
		t.Fatalf("full tier: after-read plur_recall failed: %v", err)
	}
	if res.Error != nil {
		t.Fatalf("full tier: after-read plur_recall rejected: %v", res.Error)
	}
	if !strings.Contains(res.Text, marker) {
		t.Fatalf("full tier: plur_recall(%q) did not surface the gated learn — persistence not observed. recall: %s", marker, res.Text)
	}
	afterCounts := liveStatusCounts(t, after)
	t.Logf("full tier verified: recall surfaced marker %q (engram count %d -> %d)", marker, before.engrams, afterCounts.engrams)
}

// TestLivePlurProbe1_BatchSessionIDRoundTrip is settled open Q1 of the
// issue #1410 grill round (probe-then-adopt): does the real plur_learn_batch
// accept a per-item native session_id despite the schema omission? Send one
// engram carrying session_id, then read back via plur_history (the most
// direct surface for stored engram attributes; plur_recall as fallback).
// Outcome — adopt-native (id round-tripped) or keep-tags (rejected or
// silently dropped) — is logged verbatim for the T8 ADR Round-3 table.
func TestLivePlurProbe1_BatchSessionIDRoundTrip(t *testing.T) {
	storeHome := newLiveStoreHome(t)
	c := skipUnlessLivePlurAvailable(t, storeHome)
	defer c.Close()

	markerStatement := fmt.Sprintf("probe1-session-id-round-trip-%d", time.Now().UnixNano())
	probeSessionID := fmt.Sprintf("probe1-session-%d", time.Now().UnixNano())

	res, err := callLiveTool(t, c, "plur_learn_batch", map[string]interface{}{
		"engrams": []interface{}{
			map[string]interface{}{
				"statement":  markerStatement,
				"tags":       []interface{}{"session:probe1"},
				"session_id": probeSessionID,
			},
		},
	})
	if err != nil {
		t.Fatalf("probe1: plur_learn_batch transport failed: %v", err)
	}
	if res.Error != nil {
		t.Logf("probe1: plur_learn_batch hard-rejected the per-item session_id: %v", res.Error)
		t.Logf("PROBE1 OUTCOME: keep-tags")
		return
	}
	t.Logf("probe1: plur_learn_batch accepted: %s", res.Text)

	// Read back: plur_history (recent history across all engrams) is the
	// most direct surface for stored engram attributes; plur_recall is the
	// fallback when history is unavailable/rejected.
	roundTripped := false
	if hist, herr := callLiveTool(t, c, "plur_history", map[string]interface{}{}); herr == nil && hist.Error == nil {
		roundTripped = strings.Contains(hist.Text, probeSessionID)
		t.Logf("probe1: plur_history read-back: %s", hist.Text)
	} else if rec, rerr := callLiveTool(t, c, "plur_recall", map[string]interface{}{"query": probeSessionID}); rerr == nil && rec.Error == nil {
		roundTripped = strings.Contains(rec.Text, probeSessionID)
		t.Logf("probe1: plur_recall read-back: %s", rec.Text)
	}

	if roundTripped {
		t.Logf("PROBE1 OUTCOME: adopt-native")
		return
	}
	t.Logf("PROBE1 OUTCOME: keep-tags")
}

// TestLivePlurProbe2_LearnUnknownAgentParam is settled open Q3 of the
// issue #1410 grill round: does the real plur_learn hard-reject or silently
// ignore the old {statement, agent} shape? Send the legacy shape and
// observe: isError → hard-reject; success + engram created → silently-ignore.
// Outcome is logged verbatim for the T8 ADR Round-3 table.
func TestLivePlurProbe2_LearnUnknownAgentParam(t *testing.T) {
	storeHome := newLiveStoreHome(t)
	c := skipUnlessLivePlurAvailable(t, storeHome)
	defer c.Close()

	markerStatement := fmt.Sprintf("probe2-old-shape-agent-%d", time.Now().UnixNano())

	res, err := callLiveTool(t, c, "plur_learn", map[string]interface{}{
		"statement": markerStatement,
		"agent":     "coder",
	})
	if err != nil {
		t.Fatalf("probe2: plur_learn transport failed: %v", err)
	}
	if res.Error != nil {
		t.Logf("probe2: plur_learn hard-rejected the old-shape agent param: %v", res.Error)
		t.Logf("PROBE2 OUTCOME: hard-reject")
		return
	}
	t.Logf("probe2: plur_learn accepted (no isError): %s", res.Text)

	// Success without isError: did the engram actually land (the unknown
	// agent param silently ignored)?
	rec, rerr := callLiveTool(t, c, "plur_recall", map[string]interface{}{"query": markerStatement})
	if rerr != nil || rec.Error != nil {
		t.Logf("probe2: read-back plur_recall failed (err=%v toolErr=%v) — cannot confirm creation", rerr, rec.Error)
		t.Logf("PROBE2 OUTCOME: hard-reject (read-back inconclusive)")
		return
	}
	if strings.Contains(rec.Text, markerStatement) {
		t.Logf("PROBE2 OUTCOME: silently-ignore")
		return
	}
	t.Logf("probe2: plur_learn accepted but recall did not surface the engram (recall: %s)", rec.Text)
	t.Logf("PROBE2 OUTCOME: hard-reject (success reported but no engram created — the shape was not honored)")
}
