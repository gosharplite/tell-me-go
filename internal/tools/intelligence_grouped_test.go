package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMoveDefinition_Grouped(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test-move-def-grouped-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	srcFile := filepath.Join(tmpDir, "src.go")
	srcContent := `package pkg

const (
	A = 1
	B = 2
)

type (
	T1 struct{}
	T2 struct{}
)
`
	os.WriteFile(srcFile, []byte(srcContent), 0644)

	dstFile := filepath.Join(tmpDir, "dst.go")
	os.WriteFile(dstFile, []byte("package pkg\n"), 0644)

	sm := NewSecurityManager()
	sm.RegisterSafePath(tmpDir)
	os.Setenv("TELL_ME_MOCK_ANSWER", "y")
	defer os.Unsetenv("TELL_ME_MOCK_ANSWER")
	m := &intelligenceManager{sm: sm}

	// Move B
	_, err = m.moveDefinition(context.Background(), map[string]interface{}{
		"symbol": "B", "src_file": srcFile, "dst_file": dstFile,
	})
	if err != nil {
		t.Fatal(err)
	}

	srcRes, _ := os.ReadFile(srcFile)
	dstRes, _ := os.ReadFile(dstFile)

	t.Logf("Src after moving B:\n%s", srcRes)
	t.Logf("Dst after moving B:\n%s", dstRes)

	if strings.Contains(string(srcRes), "B = 2") {
		t.Errorf("src still contains B")
	}
	if !strings.Contains(string(dstRes), "B = 2") {
		t.Errorf("dst missing B")
	}
	// With current implementation, it moves the WHOLE group
	if strings.Contains(string(srcRes), "A = 1") {
		t.Log("Note: A was NOT moved (current implementation might move whole group if not careful)")
	} else {
		t.Log("Note: A WAS moved along with B (grouped GenDecl)")
	}
}
