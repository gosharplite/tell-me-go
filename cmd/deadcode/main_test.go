// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

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

func TestRun_ErrorPath(t *testing.T) {
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { os.Chdir(oldWd) })

	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	oldStderr := os.Stderr
	t.Cleanup(func() { os.Stderr = oldStderr })
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w

	exitCode := run()

	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)

	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1", exitCode)
	}
	if !strings.Contains(buf.String(), "Error:") {
		t.Errorf("stderr should contain 'Error:', got %q", buf.String())
	}
}

func TestRun_Success(t *testing.T) {
	oldStdout := os.Stdout
	t.Cleanup(func() { os.Stdout = oldStdout })
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	exitCode := run()

	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)

	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0", exitCode)
	}
	if buf.Len() == 0 {
		t.Error("stdout should not be empty")
	}
}
