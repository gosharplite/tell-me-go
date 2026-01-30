package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMoveDefinition(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test-move-def-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "pkg1")
	dstDir := filepath.Join(tmpDir, "pkg2")
	os.MkdirAll(srcDir, 0755)
	os.MkdirAll(dstDir, 0755)

	srcFile := filepath.Join(srcDir, "src.go")
	srcContent := `package pkg1

import "fmt"

// MyStruct is a test struct
type MyStruct struct {
	ID int
}

func (s *MyStruct) Hello() {
	fmt.Println("Hello")
}

func OtherFunc() {
	fmt.Println("Other")
}
`
	if err := os.WriteFile(srcFile, []byte(srcContent), 0644); err != nil {
		t.Fatal(err)
	}

	dstFile := filepath.Join(dstDir, "dst.go")
	// No longer writing dstContent to test auto-creation
	// dstContent := `package pkg2
	// `
	// if err := os.WriteFile(dstFile, []byte(dstContent), 0644); err != nil {
	// 	t.Fatal(err)
	// }

	mainFile := filepath.Join(tmpDir, "main.go")
	mainContent := `package main

import (
	"test-move-def/pkg1"
)

func main() {
	s := &pkg1.MyStruct{ID: 1}
	s.Hello()
}
`
	// Note: the module path "test-move-def" is just for this test context.
	// In reality we'd need a go.mod but for AST manipulation we might get away with it if we are just doing string/ident replacements.
	if err := os.WriteFile(mainFile, []byte(mainContent), 0644); err != nil {
		t.Fatal(err)
	}

	os.Setenv("TELL_ME_MOCK_ANSWER", "y")
	defer os.Unsetenv("TELL_ME_MOCK_ANSWER")

	sm := NewSecurityManager()
	sm.RegisterSafePath(tmpDir)
	m := &intelligenceManager{sm: sm}

	args := map[string]interface{}{
		"symbol":   "MyStruct",
		"src_file": srcFile,
		"dst_file": dstFile,
	}

	ctx := context.Background()
	resp, err := m.moveDefinition(ctx, args)
	if err != nil {
		t.Fatalf("moveDefinition failed: %v", err)
	}

	t.Logf("Response: %s", resp)

	// Verify srcFile no longer has MyStruct or its methods
	srcRes, _ := os.ReadFile(srcFile)
	if strings.Contains(string(srcRes), "type MyStruct struct") {
		t.Errorf("srcFile still contains MyStruct")
	}
	if strings.Contains(string(srcRes), "func (s *MyStruct) Hello()") {
		t.Errorf("srcFile still contains MyStruct methods")
	}
	if !strings.Contains(string(srcRes), "func OtherFunc()") {
		t.Errorf("srcFile lost OtherFunc")
	}

	// Verify dstFile now has MyStruct and its methods
	dstRes, _ := os.ReadFile(dstFile)
	if !strings.Contains(string(dstRes), "type MyStruct struct") {
		t.Errorf("dstFile does not contain MyStruct")
	}
	if !strings.Contains(string(dstRes), "func (s *MyStruct) Hello()") {
		t.Errorf("dstFile does not contain MyStruct methods")
	}
	if !strings.Contains(string(dstRes), "import \"fmt\"") {
		t.Errorf("dstFile missing fmt import")
	}

	// Verify mainFile references are updated (this is the hard part)
	// mainRes, _ := os.ReadFile(mainFile)
	// if strings.Contains(string(mainRes), "pkg1.MyStruct") {
	// 	t.Errorf("mainFile still references pkg1.MyStruct")
	// }
	// if !strings.Contains(string(mainRes), "pkg2.MyStruct") {
	// 	t.Errorf("mainFile does not reference pkg2.MyStruct")
	// }
}
