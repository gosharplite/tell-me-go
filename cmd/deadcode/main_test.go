// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/race"

	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/tools/analysis"
)

// mockDeadCodeAnalyzer is a test double for the deadCodeAnalyzer interface.
type mockDeadCodeAnalyzer struct {
	reports        []analysis.OrphanReport
	exitCandidates []analysis.ExitCandidate
	err            error
}

func (m *mockDeadCodeAnalyzer) GatherOrphanReports(ctx context.Context, root string, deep bool, heartbeat chan<- struct{}) ([]analysis.OrphanReport, error) {
	return m.reports, m.err
}

func (m *mockDeadCodeAnalyzer) GatherExitCandidates(ctx context.Context, root string, heartbeat chan<- struct{}) ([]analysis.ExitCandidate, error) {
	return m.exitCandidates, m.err
}

func TestMain_Execution(t *testing.T) {
	if race.Enabled {
		t.Skip("skipping self-exec test under race detector: child inherits -race and times out on full-module AST analysis")
	}
	if os.Getenv("TEST_DEADCODE_MAIN") == "1" {
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestMain_Execution")
	cmd.Env = append(os.Environ(), "TEST_DEADCODE_MAIN=1")
	err := cmd.Run()
	if err != nil {
		t.Fatalf("process ran with err %v, want exit status 0", err)
	}
}

func TestRun_Error(t *testing.T) {
	origNewAnalyzer := newAnalyzer
	t.Cleanup(func() { newAnalyzer = origNewAnalyzer })

	injectedErr := errors.New("boom")
	newAnalyzer = func(sp domain_security.PathValidator) deadCodeAnalyzer {
		return &mockDeadCodeAnalyzer{err: injectedErr}
	}

	oldStderr := os.Stderr
	t.Cleanup(func() { os.Stderr = oldStderr })
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w

	exitCode := run()

	if err := w.Close(); err != nil {
		t.Logf("closing stderr pipe writer: %v", err)
	}
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1", exitCode)
	}
	stderr := buf.String()
	if !strings.Contains(stderr, "Error: boom") {
		t.Errorf("stderr = %q, want it to contain %q", stderr, "Error: boom")
	}
}

func TestRun_NoDeadCode(t *testing.T) {
	origNewAnalyzer := newAnalyzer
	t.Cleanup(func() { newAnalyzer = origNewAnalyzer })

	newAnalyzer = func(sp domain_security.PathValidator) deadCodeAnalyzer {
		return &mockDeadCodeAnalyzer{reports: []analysis.OrphanReport{}}
	}

	oldStdout := os.Stdout
	t.Cleanup(func() { os.Stdout = oldStdout })
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	exitCode := run()

	if err := w.Close(); err != nil {
		t.Logf("closing stdout pipe writer: %v", err)
	}
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0", exitCode)
	}
	stdout := buf.String()
	if !strings.Contains(stdout, "No dead code found.") {
		t.Errorf("stdout = %q, want it to contain %q", stdout, "No dead code found.")
	}
}

func TestRun_ReportsFound(t *testing.T) {
	origNewAnalyzer := newAnalyzer
	t.Cleanup(func() { newAnalyzer = origNewAnalyzer })

	newAnalyzer = func(sp domain_security.PathValidator) deadCodeAnalyzer {
		return &mockDeadCodeAnalyzer{
			reports: []analysis.OrphanReport{
				{
					Severity: "DEAD",
					Pkg:      "internal/deadpkg",
					Symbol:   "DeadFunc",
					Type:     "Function",
					Reason:   "zero inbound references",
				},
				{
					Severity: "PRIVATE",
					Pkg:      "internal/privpkg",
					Symbol:   "PrivateType",
					Type:     "Type",
					Reason:   "no external usages",
				},
			},
		}
	}

	oldStdout := os.Stdout
	t.Cleanup(func() { os.Stdout = oldStdout })
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	exitCode := run()

	if err := w.Close(); err != nil {
		t.Logf("closing stdout pipe writer: %v", err)
	}
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0", exitCode)
	}
	stdout := buf.String()
	if !strings.Contains(stdout, "[DEAD] internal/deadpkg.DeadFunc (Function) — zero inbound references") {
		t.Errorf("stdout missing DEAD report line: %q", stdout)
	}
	if !strings.Contains(stdout, "[PRIVATE] internal/privpkg.PrivateType (Type) — no external usages") {
		t.Errorf("stdout missing PRIVATE report line: %q", stdout)
	}
}

func TestRun_ExitCandidatesSection(t *testing.T) {
	origNewAnalyzer := newAnalyzer
	t.Cleanup(func() { newAnalyzer = origNewAnalyzer })

	// run() dispatches on *exitQueryMode: this test exercises the exit-query
	// channel. Restore the prior value so sibling tests see the default.
	origExitQueryMode := *exitQueryMode
	*exitQueryMode = true
	t.Cleanup(func() { *exitQueryMode = origExitQueryMode })

	newAnalyzer = func(sp domain_security.PathValidator) deadCodeAnalyzer {
		return &mockDeadCodeAnalyzer{
			reports: []analysis.OrphanReport{},
			exitCandidates: []analysis.ExitCandidate{
				{
					Symbol:       "Logger",
					Pkg:          "internal/domain/ports",
					Layer:        "application",
					Consumers:    2,
					Implementers: 0,
				},
				{
					Symbol:    "Health",
					Pkg:       "internal/domain/ports",
					Layer:     "orphan (no non-di consumers or implementers)",
					Consumers: 0,
				},
			},
		}
	}

	oldStdout := os.Stdout
	t.Cleanup(func() { os.Stdout = oldStdout })
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	exitCode := run()

	if err := w.Close(); err != nil {
		t.Logf("closing stdout pipe writer: %v", err)
	}
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0 (report-only)", exitCode)
	}
	stdout := buf.String()

	// The banner must appear even though there are no orphan reports.
	if !strings.Contains(stdout, "— EXIT CANDIDATES (ADR-056 Decision 1, report-only) —") {
		t.Errorf("stdout missing exit-candidates banner: %q", stdout)
	}
	// Rows render symbol, layer, counts, and status. Logger and Health are
	// not documented ADR-056 stays → status is "NEW — adjudicate".
	if !strings.Contains(stdout, "| internal/domain/ports.Logger | application | 2 | 0 | NEW — adjudicate |") {
		t.Errorf("stdout missing exit-candidate row: %q", stdout)
	}
	if !strings.Contains(stdout, "| internal/domain/ports.Health | orphan (no non-di consumers or implementers) | 0 | 0 | NEW — adjudicate |") {
		t.Errorf("stdout missing orphan exit-candidate row: %q", stdout)
	}
}

func TestRun_ExitCandidatesSection_NoCandidates(t *testing.T) {
	origNewAnalyzer := newAnalyzer
	t.Cleanup(func() { newAnalyzer = origNewAnalyzer })

	// run() dispatches on *exitQueryMode: this test exercises the exit-query
	// channel. Restore the prior value so sibling tests see the default.
	origExitQueryMode := *exitQueryMode
	*exitQueryMode = true
	t.Cleanup(func() { *exitQueryMode = origExitQueryMode })

	newAnalyzer = func(sp domain_security.PathValidator) deadCodeAnalyzer {
		return &mockDeadCodeAnalyzer{reports: []analysis.OrphanReport{}}
	}

	oldStdout := os.Stdout
	t.Cleanup(func() { os.Stdout = oldStdout })
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	exitCode := run()

	if err := w.Close(); err != nil {
		t.Logf("closing stdout pipe writer: %v", err)
	}
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0 (report-only)", exitCode)
	}
	stdout := buf.String()
	if !strings.Contains(stdout, "— EXIT CANDIDATES (ADR-056 Decision 1, report-only) —") {
		t.Errorf("stdout missing exit-candidates banner: %q", stdout)
	}
	if !strings.Contains(stdout, "no exit candidates") {
		t.Errorf("stdout missing no-candidates note: %q", stdout)
	}
}

// TestRun_ExitQueryQuietMode pins the quiet-mode default: with -exit-query
// and no NEW candidates (the lone candidate is a documented ADR-056 stay),
// runExitQuery prints the one-line governance summary and NOT the candidate
// table — an actionable row must never be hidden, but a fully-documented
// stay list is noise.
func TestRun_ExitQueryQuietMode(t *testing.T) {
	origNewAnalyzer := newAnalyzer
	t.Cleanup(func() { newAnalyzer = origNewAnalyzer })

	origExitQueryMode := *exitQueryMode
	origExitQueryVerbose := *exitQueryVerbose
	*exitQueryMode = true
	*exitQueryVerbose = false
	t.Cleanup(func() {
		*exitQueryMode = origExitQueryMode
		*exitQueryVerbose = origExitQueryVerbose
	})

	newAnalyzer = func(sp domain_security.PathValidator) deadCodeAnalyzer {
		return &mockDeadCodeAnalyzer{
			exitCandidates: []analysis.ExitCandidate{
				{
					Symbol:       "Capturer", // documented ADR-056 stay
					Pkg:          "internal/domain/ports",
					Layer:        "application",
					Consumers:    28,
					Implementers: 416,
				},
			},
		}
	}

	oldStdout := os.Stdout
	t.Cleanup(func() { os.Stdout = oldStdout })
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	exitCode := run()

	if err := w.Close(); err != nil {
		t.Logf("closing stdout pipe writer: %v", err)
	}
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0 (report-only)", exitCode)
	}
	stdout := buf.String()
	if !strings.Contains(stdout, "documented stay(s)") {
		t.Errorf("stdout missing quiet governance summary: %q", stdout)
	}
	if strings.Contains(stdout, "| symbol |") {
		t.Errorf("quiet mode must not print the candidate table: %q", stdout)
	}
}

// TestRun_ExitQueryVerbose pins the explicit verbose override: with
// -exit-query -exit-query-verbose, the full candidate table always prints
// even when every candidate is a documented stay.
func TestRun_ExitQueryVerbose(t *testing.T) {
	origNewAnalyzer := newAnalyzer
	t.Cleanup(func() { newAnalyzer = origNewAnalyzer })

	origExitQueryMode := *exitQueryMode
	origExitQueryVerbose := *exitQueryVerbose
	*exitQueryMode = true
	*exitQueryVerbose = true
	t.Cleanup(func() {
		*exitQueryMode = origExitQueryMode
		*exitQueryVerbose = origExitQueryVerbose
	})

	newAnalyzer = func(sp domain_security.PathValidator) deadCodeAnalyzer {
		return &mockDeadCodeAnalyzer{
			exitCandidates: []analysis.ExitCandidate{
				{
					Symbol:       "Capturer", // documented ADR-056 stay
					Pkg:          "internal/domain/ports",
					Layer:        "application",
					Consumers:    28,
					Implementers: 416,
				},
			},
		}
	}

	oldStdout := os.Stdout
	t.Cleanup(func() { os.Stdout = oldStdout })
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	exitCode := run()

	if err := w.Close(); err != nil {
		t.Logf("closing stdout pipe writer: %v", err)
	}
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0 (report-only)", exitCode)
	}
	stdout := buf.String()
	if !strings.Contains(stdout, "— EXIT CANDIDATES (ADR-056 Decision 1, report-only) —") {
		t.Errorf("stdout missing exit-candidates banner: %q", stdout)
	}
	if !strings.Contains(stdout, "| symbol |") {
		t.Errorf("verbose mode must print the full candidate table: %q", stdout)
	}
}

// TestRun_ExitQueryError covers the exit-query ERROR path in runExitQuery:
// with -exit-query and a failing analyzer, run() dispatches to the exit
// query channel, prints "Error: boom" to stderr, and returns exit code 1.
func TestRun_ExitQueryError(t *testing.T) {
	origNewAnalyzer := newAnalyzer
	t.Cleanup(func() { newAnalyzer = origNewAnalyzer })

	// run() dispatches on *exitQueryMode: this test exercises the exit-query
	// ERROR path. Restore the prior value so sibling tests see the default.
	origExitQueryMode := *exitQueryMode
	*exitQueryMode = true
	t.Cleanup(func() { *exitQueryMode = origExitQueryMode })

	injectedErr := errors.New("boom")
	newAnalyzer = func(sp domain_security.PathValidator) deadCodeAnalyzer {
		return &mockDeadCodeAnalyzer{err: injectedErr}
	}

	oldStderr := os.Stderr
	t.Cleanup(func() { os.Stderr = oldStderr })
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w

	exitCode := run()

	if err := w.Close(); err != nil {
		t.Logf("closing stderr pipe writer: %v", err)
	}
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1", exitCode)
	}
	stderr := buf.String()
	if !strings.Contains(stderr, "Error: boom") {
		t.Errorf("stderr = %q, want it to contain %q", stderr, "Error: boom")
	}
}
