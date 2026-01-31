// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListSymbols(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSecurityManager()
	sm.RegisterSafePath(tmpDir)
	m := &intelligenceManager{sm: sm}

	code := `package test
const MyConst = 1
var MyVar = 2
type MyType struct{}
func MyFunc() {}
func (m MyType) MyMethod() {}
func privateFunc() {}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "symbols.go"), []byte(code), 0644); err != nil {
		t.Fatal(err)
	}

	t.Run("All Symbols", func(t *testing.T) {
		args := map[string]interface{}{"path": tmpDir}
		res, err := m.listSymbols(context.Background(), args)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "MyConst") || !strings.Contains(res.Text, "privateFunc") {
			t.Errorf("Missing symbols: %s", res.Text)
		}
	})

	t.Run("Exported Only", func(t *testing.T) {
		args := map[string]interface{}{"path": tmpDir, "exported_only": true}
		res, err := m.listSymbols(context.Background(), args)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "MyFunc") {
			t.Errorf("Missing exported symbol: %s", res.Text)
		}
		if strings.Contains(res.Text, "privateFunc") {
			t.Errorf("Found private symbol: %s", res.Text)
		}
	})
}
