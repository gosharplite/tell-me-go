package analysis

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/tools/code/astutil"
	"github.com/gosharplite/tell-me-go/internal/tools/code/index"
)

func TestTypeManager_GetTypeInfo(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "typemanager-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	err = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.24"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	code := `package test
// MyStruct is a test struct
type MyStruct struct {
	ID int ` + "`" + `json:"id"` + "`" + `
}
func (s *MyStruct) Foo() {}
`
	filePath := filepath.Join(tmpDir, "test.go")
	if err := os.WriteFile(filePath, []byte(code), 0644); err != nil {
		t.Fatal(err)
	}

	idx, _ := index.NewIndexer(tmpDir)
	cache := astutil.NewASTCache()
	sp := &mockSecurityProvider{}
	m := NewTypeManager(idx, cache, sp)

	res, err := m.GetTypeInfo(context.Background(), map[string]interface{}{"typename": "MyStruct"})
	if err != nil {
		t.Fatalf("GetTypeInfo failed: %v", err)
	}

	if !strings.Contains(res.Text, "Type: MyStruct") {
		t.Errorf("Expected result to contain Type: MyStruct. Got:\n%s", res.Text)
	}
	if !strings.Contains(res.Text, "ID int `json:\"id\"`") {
		t.Errorf("Expected result to contain field info. Got:\n%s", res.Text)
	}
	if !strings.Contains(res.Text, "func (s *MyStruct) Foo()") {
		t.Errorf("Expected result to contain method info. Got:\n%s", res.Text)
	}
}
