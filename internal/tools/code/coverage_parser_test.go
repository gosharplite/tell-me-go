// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package code

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCoverageProfile(t *testing.T) {
	input := `mode: set
github.com/gosharplite/tell-me-go/internal/service/user.go:84.2,86.12 3 0
github.com/gosharplite/tell-me-go/internal/service/user.go:88.2,90.12 2 1
github.com/gosharplite/tell-me-go/internal/service/auth.go:10.5,12.10 4 0
`
	r := strings.NewReader(input)
	blocks, err := ParseCoverageProfile(r)
	if err != nil {
		t.Fatalf("ParseCoverageProfile failed: %v", err)
	}

	if len(blocks) != 2 {
		t.Errorf("expected 2 uncovered blocks, got %d", len(blocks))
	}

	expected := []UncoveredBlock{
		{File: "internal/service/user.go", Start: 84, End: 86, Stmts: 3},
		{File: "internal/service/auth.go", Start: 10, End: 12, Stmts: 4},
	}

	for i, b := range blocks {
		if b.File != expected[i].File || b.Start != expected[i].Start || b.End != expected[i].End || b.Stmts != expected[i].Stmts {
			t.Errorf("block %d: expected %+v, got %+v", i, expected[i], b)
		}
	}
}

func TestUncoveredBlock_Classify(t *testing.T) {
	tests := []struct {
		name             string
		code             string
		file             string
		expectedCategory string
		expectedPriority string
	}{
		{
			name:             "Business logic with error handling",
			code:             "if err != nil { return err }",
			file:             "internal/service/user.go",
			expectedCategory: "ERROR_HANDLING",
			expectedPriority: "High",
		},
		{
			name:             "Adapter with error handling",
			code:             "if err != nil { return fmt.Errorf(\"fail: %w\", err) }",
			file:             "internal/repository/db.go",
			expectedCategory: "ERROR_HANDLING",
			expectedPriority: "Medium",
		},
		{
			name:             "Generic business logic",
			code:             "x := a + b\nreturn x",
			file:             "internal/usecase/calc.go",
			expectedCategory: "BUSINESS_LOGIC",
			expectedPriority: "Medium",
		},
		{
			name:             "Other/Miscellaneous",
			code:             "package main\nfunc main() {}",
			file:             "cmd/tools/main.go",
			expectedCategory: "OTHER",
			expectedPriority: "Low",
		},
		{
			name:             "Adapter without error handling",
			code:             "func Save() {}",
			file:             "internal/gateway/api.go",
			expectedCategory: "ADAPTER",
			expectedPriority: "Low",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &UncoveredBlock{
				Code: tt.code,
				File: tt.file,
			}
			b.Classify()
			if b.Category != tt.expectedCategory {
				t.Errorf("expected category %s, got %s", tt.expectedCategory, b.Category)
			}
			if b.Priority != tt.expectedPriority {
				t.Errorf("expected priority %s, got %s", tt.expectedPriority, b.Priority)
			}
		})
	}
}

func TestUncoveredBlock_ExtractCode(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.go")
	content := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10"
	err := os.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	b := &UncoveredBlock{
		File:  filePath,
		Start: 5,
		End:   7,
	}

	err = b.ExtractCode()
	if err != nil {
		t.Fatalf("ExtractCode failed: %v", err)
	}

	// Should include 1 line of padding: line 4, 5, 6, 7
	expected := "line4\nline5\nline6\nline7"
	if b.Code != expected {
		t.Errorf("expected code:\n%s\ngot:\n%s", expected, b.Code)
	}
}

func TestGetDetailedCoverage_Mocked(t *testing.T) {
	mockRunner := func(name string, arg ...string) ([]byte, error) {
		// Find -coverprofile
		var profilePath string
		for _, a := range arg {
			if strings.HasPrefix(a, "-coverprofile=") {
				profilePath = strings.TrimPrefix(a, "-coverprofile=")
				break
			}
		}

		if profilePath != "" {
			content := "mode: set\nmain.go:1.1,2.1 1 0\n"
			err := os.WriteFile(profilePath, []byte(content), 0644)
			if err != nil {
				return nil, err
			}
		}

		return []byte("ok"), nil
	}

	blocks, err := GetDetailedCoverage("./...", mockRunner)
	if err != nil {
		t.Fatalf("GetDetailedCoverage failed: %v", err)
	}

	// We expect 1 block from the mock profile. 
	// Note: ExtractCode will fail because main.go likely doesn't exist in the current test context 
	// but ExtractCode error is ignored in GetDetailedCoverage loop.
	if len(blocks) != 1 {
		t.Errorf("expected 1 block, got %d", len(blocks))
	}
}

func TestGetDetailedCoverageReport(t *testing.T) {
	mockRunner := func(name string, arg ...string) ([]byte, error) {
		var profilePath string
		for _, a := range arg {
			if strings.HasPrefix(a, "-coverprofile=") {
				profilePath = strings.TrimPrefix(a, "-coverprofile=")
				break
			}
		}

		if profilePath != "" {
			// Create 2 High, 1 Medium, 5 Low blocks
			content := "mode: set\n"
			// High (Business + Error)
			content += "github.com/gosharplite/tell-me-go/internal/service/user.go:1.1,2.1 1 0\n"
			content += "github.com/gosharplite/tell-me-go/internal/service/order.go:1.1,2.1 1 0\n"
			// Medium (Technical Debt - Business no Error)
			content += "github.com/gosharplite/tell-me-go/internal/service/meta.go:1.1,2.1 1 0\n"
			// Low
			for i := 0; i < 5; i++ {
				content += "cmd/tool/main.go:1.1,2.1 1 0\n"
			}
			err := os.WriteFile(profilePath, []byte(content), 0644)
			if err != nil {
				return nil, err
			}
		}
		return []byte("ok"), nil
	}

	// We need to make sure the "code" contains "err" for classification to pick up ERROR_HANDLING
	// But GetDetailedCoverage calls ExtractCode, which will fail to find these files.
	// So we'll have to rely on the fact that without file content, they might not be classified as we want 
	// OR we create the files in a temp dir and point to them.
	
	// Better: Use GetDetailedCoverageReport but mock the runner to provide a profile, 
	// then we might need to manually adjust blocks if we want to test the report formatting precisely 
	// without creating many files.
	
	// Actually, let's test the formatting logic by calling a version that doesn't run the command 
	// if we had one. Since we don't, we'll do our best with the runner.
	
	report, err := GetDetailedCoverageReport("./...", mockRunner)
	if err != nil {
		t.Fatalf("GetDetailedCoverageReport failed: %v", err)
	}

	// Check for key markers
	markers := []string{
		"Detailed Coverage Report",
		"Summary:",
		"Total Gaps:",
		"High Priority",
		"Medium Priority",
		"Low Priority",
	}

	for _, m := range markers {
		if !strings.Contains(report, m) {
			t.Errorf("report missing marker: %s", m)
		}
	}
}

func TestGetDetailedCoverageReport_Specifics(t *testing.T) {
	// Test the formatting logic directly by constructing blocks
	// Since we can't inject blocks directly into the report function without another refactor,
	// let's ensure our mock actually produces these categories by having a better mock runner.
	
	tmpDir := t.TempDir()
	f1Path := filepath.Join(tmpDir, "internal/service/user.go")
	_ = os.MkdirAll(filepath.Dir(f1Path), 0755)
	_ = os.WriteFile(f1Path, []byte("if err != nil"), 0644)
	
	f2Path := filepath.Join(tmpDir, "internal/service/meta.go")
	_ = os.MkdirAll(filepath.Dir(f2Path), 0755)
	_ = os.WriteFile(f2Path, []byte("package meta"), 0644)

	// Change working directory to temp dir so relative paths in profile work with ExtractCode
	oldWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

	mockRunner := func(name string, arg ...string) ([]byte, error) {
		var profilePath string
		for _, a := range arg {
			if strings.HasPrefix(a, "-coverprofile=") {
				profilePath = strings.TrimPrefix(a, "-coverprofile=")
				break
			}
		}
		if profilePath != "" {
			content := "mode: set\n"
			content += "internal/service/user.go:1.1,2.1 1 0\n"
			content += "internal/service/meta.go:1.1,2.1 1 0\n"
			_ = os.WriteFile(profilePath, []byte(content), 0644)
		}
		return []byte("ok"), nil
	}

	report, err := GetDetailedCoverageReport("./...", mockRunner)
	if err != nil {
		t.Fatalf("GetDetailedCoverageReport failed: %v", err)
	}

	if !strings.Contains(report, "[HIGH PRIORITY GAPS]") {
		t.Errorf("report missing [HIGH PRIORITY GAPS]")
	}
}

func TestShellRunner(t *testing.T) {
	// We can't easily test ShellRunner without executing a real command.
	// We'll just verify it doesn't crash for a simple command.
	_, err := ShellRunner("go", "version")
	if err != nil {
		t.Errorf("ShellRunner failed: %v", err)
	}
}

func TestGetDetailedCoverage_Error(t *testing.T) {
	// Test temp file creation failure (mocking os.CreateTemp is hard, 
	// but we can test the runner error)
	errRunner := func(name string, arg ...string) ([]byte, error) {
		return nil, os.ErrPermission
	}
	
	_, err := GetDetailedCoverage("./...", errRunner)
	if err == nil {
		t.Error("expected error when runner fails to produce profile, got nil")
	}
}

func TestGetDetailedCoverage_EmptyProfile(t *testing.T) {
	emptyRunner := func(name string, arg ...string) ([]byte, error) {
		var profilePath string
		for _, a := range arg {
			if strings.HasPrefix(a, "-coverprofile=") {
				profilePath = strings.TrimPrefix(a, "-coverprofile=")
				break
			}
		}
		if profilePath != "" {
			_ = os.WriteFile(profilePath, []byte(""), 0644)
		}
		return []byte("ok"), nil
	}

	_, err := GetDetailedCoverage("./...", emptyRunner)
	if err == nil {
		t.Error("expected error for empty profile, got nil")
	} else if !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected empty profile error, got: %v", err)
	}
}

func TestGetDetailedCoverageJSON(t *testing.T) {
	mockRunner := func(name string, arg ...string) ([]byte, error) {
		var profilePath string
		for _, a := range arg {
			if strings.HasPrefix(a, "-coverprofile=") {
				profilePath = strings.TrimPrefix(a, "-coverprofile=")
				break
			}
		}
		if profilePath != "" {
			content := "mode: set\nmain.go:1.1,2.1 1 0\n"
			_ = os.WriteFile(profilePath, []byte(content), 0644)
		}
		return []byte("ok"), nil
	}

	jsonStr, err := GetDetailedCoverageJSON("./...", "Low", mockRunner)
	if err != nil {
		t.Fatalf("GetDetailedCoverageJSON failed: %v", err)
	}

	if !strings.HasPrefix(jsonStr, "[") || !strings.HasSuffix(jsonStr, "]") {
		t.Errorf("expected JSON array, got: %s", jsonStr)
	}
}
