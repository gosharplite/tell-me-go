// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package info_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/info"
)

func TestInfoTools(t *testing.T) {
	tmpDir := t.TempDir()
	sm := tools.NewSecurityManager()
	sm.RegisterSafePath(tmpDir)
	m := &info.Manager{SP: sm}

	oldWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

	t.Run("get_project_summary", func(t *testing.T) {
		os.WriteFile("go.mod", []byte("module testsummary\ngo 1.21\n"), 0644)
		os.WriteFile("main.go", []byte("package main\n"), 0644)

		res, err := m.GetProjectSummary(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "testsummary") || !strings.Contains(res.Text, "Estimated Go LOC") {
			t.Errorf("Unexpected summary output: %s", res.Text)
		}
	})

	t.Run("go_doc", func(t *testing.T) {
		args := map[string]interface{}{"symbol": "fmt.Println"}
		res, err := m.GoDoc(context.Background(), args)
		if err != nil {
			t.Logf("Skipping go_doc test: %v", err)
			return
		}
		if !strings.Contains(res.Text, "Println formats using the default formats") {
			t.Errorf("Unexpected go doc output: %s", res.Text)
		}
	})
}
