// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"regexp"
	"strings"
	"testing"
)

func TestTurnHeaderFormat(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	provider := "google"
	mode := "architect"

	// Setup mock LLM server that returns a simple text response (no tool calls)
	server := setupSimpleTextMockServer(t, provider)
	defer server.Close()

	homeDir := t.TempDir()
	configPath := createTempConfigWithMode(t, provider, server.URL, mode)
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL,
	}

	// Run the CLI with a simple prompt.
	_, stderr, err := runCommandWithEnvInDir(homeDir, env, "", "-c", configPath, "Say hello")
	if err != nil {
		t.Fatalf("CLI failed: %v\nStderr: %s", err, stderr)
	}

	// Capture stderr and strip ANSI codes.
	errOut := stripANSI(stderr)

	// Split into lines for easier analysis.
	lines := strings.Split(errOut, "\n")

	// Find indices of key lines.
	horizRule := "────────────────────────────────────────────────────────────────────────────────"
	turnLinePrefix := "╭─⠿ Turn 1 - " + mode
	payloadLineRegex := regexp.MustCompile(`^\[\d{2}:\d{2}:\d{2}\] Payload: ~\d+/\d+ tokens - ` + mode + `$`)
	actualPayloadLineRegex := regexp.MustCompile(`^\[\d{2}:\d{2}:\d{2}\] Payload: \d+/\d+ tokens - ` + mode + `$`)
	// Metrics line pattern: timestamp, provider/model in brackets, M: N H: N C: N Th: N, and trailing timing block
	metricsLineRegex := regexp.MustCompile(`^\[\d{2}:\d{2}:\d{2}\] \[[^\]]+\] M: \d+ H: \d+ C: \d+ Th: \d+.*\[.*\]$`)
	// Ready line prefix (may have optional cost summary)
	readyLinePrefix := "╰─⠿ Ready"

	var horizIdx, turnIdx, payloadIdx, actualPayloadIdx, metricsIdx, readyIdx = -1, -1, -1, -1, -1, -1
	for i, line := range lines {
		if strings.Contains(line, horizRule) {
			horizIdx = i
		}
		if strings.HasPrefix(line, turnLinePrefix) {
			turnIdx = i
		}
		if payloadLineRegex.MatchString(line) {
			payloadIdx = i
		}
		if actualPayloadLineRegex.MatchString(line) {
			actualPayloadIdx = i
		}
		if metricsLineRegex.MatchString(line) {
			metricsIdx = i
		}
		if strings.HasPrefix(line, readyLinePrefix) {
			readyIdx = i
		}
	}

	// Assertions
	if horizIdx == -1 {
		t.Errorf("Horizontal rule not found in stderr.\nStderr output:\n%s", errOut)
	} else {
		// Check empty line before horizontal rule: either horizIdx == 0 (first line) or previous line is empty.
		if horizIdx > 0 && strings.TrimSpace(lines[horizIdx-1]) != "" {
			t.Errorf("Horizontal rule should be preceded by an empty line, but preceding line is: %q", lines[horizIdx-1])
		}
	}

	if turnIdx == -1 {
		t.Errorf("Turn line not found in stderr. Expected prefix: %s", turnLinePrefix)
	}

	if payloadIdx == -1 {
		t.Errorf("Payload line not found in stderr. Expected pattern: [HH:MM:SS] Payload: ~NNN/MMM tokens - %s", mode)
	}

	if actualPayloadIdx == -1 {
		t.Errorf("Actual payload line not found in stderr. Expected pattern: [HH:MM:SS] Payload: NNN/MMM tokens - %s", mode)
	}

	if metricsIdx == -1 {
		t.Errorf("Metrics line not found in stderr. Expected pattern: [HH:MM:SS] [provider] M: N H: N C: N Th: N [...]")
	}

	if readyIdx == -1 {
		t.Errorf("Ready line not found in stderr. Expected prefix: %s", readyLinePrefix)
	}

	// Ordering: horizontal rule → turn line → (estimated) payload line → (actual payload line) → metrics line → ready line
	if horizIdx != -1 && turnIdx != -1 && horizIdx >= turnIdx {
		t.Errorf("Horizontal rule should appear before turn line. horizIdx=%d, turnIdx=%d", horizIdx, turnIdx)
	}
	if turnIdx != -1 && payloadIdx != -1 && turnIdx >= payloadIdx {
		t.Errorf("Turn line should appear before payload line. turnIdx=%d, payloadIdx=%d", turnIdx, payloadIdx)
	}
	if payloadIdx != -1 && actualPayloadIdx != -1 && payloadIdx >= actualPayloadIdx {
		t.Errorf("Estimated payload line should appear before actual payload line. estimatedIdx=%d, actualIdx=%d", payloadIdx, actualPayloadIdx)
	}
	if actualPayloadIdx != -1 && metricsIdx != -1 && actualPayloadIdx >= metricsIdx {
		t.Errorf("Actual payload line should appear before metrics line. actualIdx=%d, metricsIdx=%d", actualPayloadIdx, metricsIdx)
	}
	if payloadIdx != -1 && metricsIdx != -1 && payloadIdx >= metricsIdx {
		t.Errorf("Payload line should appear before metrics line. payloadIdx=%d, metricsIdx=%d", payloadIdx, metricsIdx)
	}
	if metricsIdx != -1 && readyIdx != -1 && metricsIdx >= readyIdx {
		t.Errorf("Metrics line should appear before ready line. metricsIdx=%d, readyIdx=%d", metricsIdx, readyIdx)
	}

	// Ensure the horizontal rule is exactly the 64‑dash line.
	if horizIdx != -1 && lines[horizIdx] != horizRule {
		t.Errorf("Horizontal rule mismatch.\nExpected: %q\nGot: %q", horizRule, lines[horizIdx])
	}

	// Ensure turn line matches exactly "╭─⠿ Turn 1 - architect" (no extra characters before)
	if turnIdx != -1 && !strings.HasPrefix(lines[turnIdx], turnLinePrefix) {
		t.Errorf("Turn line mismatch.\nExpected prefix: %q\nGot: %q", turnLinePrefix, lines[turnIdx])
	}

	// Ensure payload line matches timestamp pattern and token counts are digits.
	if payloadIdx != -1 && !payloadLineRegex.MatchString(lines[payloadIdx]) {
		t.Errorf("Payload line mismatch.\nExpected pattern: %v\nGot: %q", payloadLineRegex, lines[payloadIdx])
	}
	if actualPayloadIdx != -1 && !actualPayloadLineRegex.MatchString(lines[actualPayloadIdx]) {
		t.Errorf("Actual payload line mismatch.\nExpected pattern: %v\nGot: %q", actualPayloadLineRegex, lines[actualPayloadIdx])
	}

	// Ensure metrics line contains required components.
	if metricsIdx != -1 {
		metricsLine := lines[metricsIdx]
		if !strings.Contains(metricsLine, " M: ") ||
			!strings.Contains(metricsLine, " H: ") ||
			!strings.Contains(metricsLine, " C: ") ||
			!strings.Contains(metricsLine, " Th: ") ||
			!strings.Contains(metricsLine, " [") {
			t.Errorf("Metrics line missing required components: %q", metricsLine)
		}
	}
}
