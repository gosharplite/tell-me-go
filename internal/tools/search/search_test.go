// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package search_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/search"
)

func TestSearchTools(t *testing.T) {
	tmpDir := t.TempDir()
	sm := tools.NewSecurityManager()
	sm.RegisterSafePath(tmpDir)
	m := &search.Manager{SP: sm}

	content := `
// TODO: fix this
// BUG: broken
func main() {}
`
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(content), 0644)

	t.Run("list_todos", func(t *testing.T) {
		args := map[string]interface{}{"path": tmpDir}
		res, err := m.ListTodos(context.Background(), args)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "TODO: fix this") || !strings.Contains(res.Text, "BUG: broken") {
			t.Errorf("TODOs not found: %s", res.Text)
		}
	})

	t.Run("search_usages_globally", func(t *testing.T) {
		oldWd, _ := os.Getwd()
		os.Chdir(tmpDir)
		defer os.Chdir(oldWd)

		args := map[string]interface{}{"query": "main"}
		res, err := m.SearchUsagesGlobally(context.Background(), args)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "main.go") {
			t.Errorf("Usage not found globally: %s", res.Text)
		}
	})
}
