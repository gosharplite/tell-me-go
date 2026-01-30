package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMoveDefinition_StdLibImport(t *testing.T) {
	testDataDir := t.TempDir()

	srcDir := filepath.Join(testDataDir, "src")
	dstDir := filepath.Join(testDataDir, "dst")
	os.MkdirAll(srcDir, 0755)
	os.MkdirAll(dstDir, 0755)

	srcFile := filepath.Join(srcDir, "source.go")
	srcContent := `package src

import "net/http"

func Mover() {
	http.Get("http://example.com")
}
`
	if err := os.WriteFile(srcFile, []byte(srcContent), 0644); err != nil {
		t.Fatal(err)
	}

	dstFile := filepath.Join(dstDir, "dest.go")

	sm := NewSecurityManager()
	sm.RegisterSafePath(testDataDir)
	
	os.Setenv("TELL_ME_MOCK_ANSWER", "y")
	defer os.Unsetenv("TELL_ME_MOCK_ANSWER")

	m := &intelligenceManager{sm: sm}

	args := map[string]interface{}{
		"symbol":   "Mover",
		"src_file": srcFile,
		"dst_file": dstFile,
	}

	ctx := context.Background()
	_, err := m.moveDefinition(ctx, args)
	if err != nil {
		t.Fatalf("moveDefinition failed: %v", err)
	}

	dstContent, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatal(err)
	}
	
	content := string(dstContent)
	t.Logf("Destination Content:\n%s", content)

	if !strings.Contains(content, "\"net/http\"") {
		t.Error("FAILURE: net/http import missing.")
	} else {
		t.Log("SUCCESS: net/http import found.")
	}
}
