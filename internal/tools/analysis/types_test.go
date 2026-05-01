package analysis

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTypeManager_GetTypeInfo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		code     string
		typename string
		want     []string
		wantErr  bool
	}{
		{
			name: "basic struct",
			code: `package test
// MyStruct is a test struct
type MyStruct struct {
	ID int ` + "`" + `json:"id"` + "`" + `
}
func (s *MyStruct) Foo() {}
`,
			typename: "MyStruct",
			want:     []string{"Type: MyStruct", "ID int `json:\"id\"`", "func (s *MyStruct) Foo()"},
		},
		{
			name: "interface type",
			code: `package test
// Reader is an interface
type Reader interface {
	Read(p []byte) (n int, err error)
}`,
			typename: "Reader",
			want:     []string{"Type: Reader", "Methods:", "Read"},
		},
		{
			name: "type alias",
			code: `package test
type MyInt int`,
			typename: "MyInt",
			want:     []string{"Type: MyInt", "Kind: alias"},
		},
		{
			name: "interface type with embedding",
			code: `package test
type I1 interface { M1() }
type I2 interface {
	I1
	M2()
}`,
			typename: "I2",
			want:     []string{"Type: I2", "M2"},
		},
		{
			name: "empty typename",
			code: `package test`,
			want: []string{"Please provide a typename."},
		},
		{
			name:     "missing type",
			code:     `package test`,
			typename: "Missing",
			want:     []string{"Type not found."},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()
			err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.25"), 0644)
			if err != nil {
				t.Fatal(err)
			}

			if tt.code != "" {
				if err := os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte(tt.code), 0644); err != nil {
					t.Fatal(err)
				}
			}

			idx, _ := newIndexer(tmpDir)
			cache := newASTCache(".")
			sp := &mockSecurityProvider{}
			m := newTypeManager(idx, cache, sp)

			res, err := m.GetTypeInfo(context.Background(), map[string]interface{}{"typename": tt.typename}, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetTypeInfo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			for _, w := range tt.want {
				if !strings.Contains(res.Text, w) {
					t.Errorf("Expected result to contain %q. Got:\n%s", w, res.Text)
				}
			}
		})
	}
}

func TestTypeManager_ListImplementations(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.25"), 0644); err != nil {
		t.Fatal(err)
	}
	code := `package test
type I interface { M() }
type S struct{}
func (s S) M() {}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte(code), 0644); err != nil {
		t.Fatal(err)
	}

	idx, _ := newIndexer(tmpDir)
	m := newTypeManager(idx, newASTCache("."), &mockSecurityProvider{})

	res, err := m.ListImplementations(context.Background(), map[string]interface{}{"interface_name": "I"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "S") {
		t.Errorf("Expected result to contain S, got:\n%s", res.Text)
	}
}

func TestTypeManager_ListSymbols(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.25"), 0644); err != nil {
		t.Fatal(err)
	}
	code := `package p
func F() {}
type T struct{}
var V int
const C = 1
`
	if err := os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte(code), 0644); err != nil {
		t.Fatal(err)
	}

	idx, _ := newIndexer(tmpDir)
	m := newTypeManager(idx, newASTCache("."), &mockSecurityProvider{})
	res, err := m.ListSymbols(context.Background(), map[string]interface{}{"path": tmpDir}, nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, s := range []string{"func F()", "type T", "var V", "C"} {
		if !strings.Contains(res.Text, s) {
			t.Errorf("Expected symbol %q in output, got:\n%s", s, res.Text)
		}
	}
}

func TestTypeManager_FindUsages(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.25"), 0644); err != nil {
		t.Fatal(err)
	}
	code := `package p
func F() {
	F()
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte(code), 0644); err != nil {
		t.Fatal(err)
	}

	idx, _ := newIndexer(tmpDir)
	m := newTypeManager(idx, newASTCache("."), &mockSecurityProvider{})
	res, err := m.FindUsages(context.Background(), map[string]interface{}{"path": tmpDir, "query": "F"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Count(res.Text, "test.go") < 1 {
		t.Errorf("Expected at least 1 usage of F, got:\n%s", res.Text)
	}
}

func TestTypeManager_FindDefinitions(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.25"), 0644); err != nil {
		t.Fatal(err)
	}
	code := `package p
func MyFunc() {}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte(code), 0644); err != nil {
		t.Fatal(err)
	}

	idx, _ := newIndexer(tmpDir)
	m := newTypeManager(idx, newASTCache("."), &mockSecurityProvider{})
	res, err := m.FindDefinitions(context.Background(), map[string]interface{}{"path": tmpDir, "query": "MyFunc"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(res.Text, "func MyFunc()") {
		t.Errorf("Expected definition of MyFunc, got:\n%s", res.Text)
	}
}

func TestTypeManager_ListImplementations_Error(t *testing.T) {
	t.Parallel()
	idx, _ := newIndexer(".")
	m := newTypeManager(idx, nil, &mockSecurityProvider{})
	res, err := m.ListImplementations(context.Background(), map[string]interface{}{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "Please provide an interface_name.") {
		t.Errorf("Expected error message for missing interface_name")
	}
}

func TestComplexityAnalyzer_Analyze_Empty(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	analyzer := newComplexityAnalyzer(newASTCache("."), &mockSecurityProvider{})
	res, err := analyzer.Analyze(context.Background(), map[string]interface{}{"path": tmpDir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "No Go functions found to analyze.") {
		t.Errorf("Expected empty result message, got: %s", res.Text)
	}
}

func TestTypeManager_ListSymbols_ExportedOnly(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.25"), 0644); err != nil {
		t.Fatal(err)
	}
	code := `package p
func Exported() {}
func unexported() {}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte(code), 0644); err != nil {
		t.Fatal(err)
	}

	idx, _ := newIndexer(tmpDir)
	m := newTypeManager(idx, newASTCache("."), &mockSecurityProvider{})
	res, err := m.ListSymbols(context.Background(), map[string]interface{}{"path": tmpDir, "exported_only": true}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(res.Text, "func Exported()") {
		t.Errorf("Expected Exported in output")
	}
	if strings.Contains(res.Text, "unexported") {
		t.Errorf("Did not expect unexported in output")
	}
}
