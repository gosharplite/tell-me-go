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

	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/tools/analysis"
)

// mockDeadCodeAnalyzer is a test double for the deadCodeAnalyzer interface.
type mockDeadCodeAnalyzer struct {
	reports []analysis.OrphanReport
	err     error
}

func (m *mockDeadCodeAnalyzer) GatherOrphanReports(ctx context.Context, root string, deep bool, heartbeat chan<- struct{}) ([]analysis.OrphanReport, error) {
	return m.reports, m.err
}

func TestMain_Execution(t *testing.T) {
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
