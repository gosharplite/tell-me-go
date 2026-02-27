package analysis

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestUncoveredBlock_Classify(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		block    uncoveredBlock
		wantCat  string
		wantPrio string
	}{
		{
			name: "error handling high priority",
			block: uncoveredBlock{
				File: "internal/domain/service.go",
				Code: "if err != nil { return err }",
			},
			wantCat:  "ERROR_HANDLING",
			wantPrio: "High",
		},
		{
			name: "business logic medium priority",
			block: uncoveredBlock{
				File: "internal/service/logic.go",
				Code: "x := y + 1",
			},
			wantCat:  "BUSINESS_LOGIC",
			wantPrio: "Medium",
		},
		{
			name: "adapter low priority",
			block: uncoveredBlock{
				File: "internal/api/handler.go",
				Code: "w.WriteHeader(200)",
			},
			wantCat:  "ADAPTER",
			wantPrio: "Medium", // Adapter + !isErrorHandling -> Medium
		},
		{
			name: "other low priority",
			block: uncoveredBlock{
				File: "main.go",
				Code: "fmt.Println()",
			},
			wantCat:  "OTHER",
			wantPrio: "Low",
		},
		{
			name: "false positive business logic",
			block: uncoveredBlock{
				File: "third_party/some-lib/internal/agent_mock.go",
				Code: "x := 1",
			},
			wantCat:  "OTHER",
			wantPrio: "Low",
		},
		{
			name: "false positive adapter",
			block: uncoveredBlock{
				File: "external/internal/api/client.go",
				Code: "x := 1",
			},
			wantCat:  "OTHER",
			wantPrio: "Low",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.block.Classify()
			if tt.block.Category != tt.wantCat {
				t.Errorf("Classify() Category = %v, want %v", tt.block.Category, tt.wantCat)
			}
			if tt.block.Priority != tt.wantPrio {
				t.Errorf("Classify() Priority = %v, want %v", tt.block.Priority, tt.wantPrio)
			}
		})
	}
}

func TestParseCoverageLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		line    string
		prefix  string
		want    *uncoveredBlock
		wantErr bool
	}{
		{
			name:   "uncovered line",
			line:   "github.com/user/repo/pkg/file.go:10.5,12.10 3 0",
			prefix: "github.com/user/repo/",
			want: &uncoveredBlock{
				File:  "pkg/file.go",
				Start: 10,
				End:   12,
				Stmts: 3,
			},
			wantErr: false,
		},
		{
			name:    "covered line (skipped)",
			line:    "github.com/user/repo/pkg/file.go:10.5,12.10 3 1",
			prefix:  "github.com/user/repo/",
			want:    nil,
			wantErr: false,
		},
		{
			name:    "invalid line",
			line:    "invalid",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "malformed count",
			line:    "file.go:1,2 3 abc",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "malformed stmts",
			line:    "file.go:1,2 abc 0",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "invalid path in 3 fields",
			line:    "invalid-path 3 0",
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseCoverageLine(tt.line, tt.prefix)
			validateParseResult(t, got, err, tt.want, tt.wantErr)
		})
	}
}

func validateParseResult(t *testing.T, got *uncoveredBlock, err error, want *uncoveredBlock, wantErr bool) {
	t.Helper()
	if (err != nil) != wantErr {
		t.Errorf("parseCoverageLine() error = %v, wantErr %v", err, wantErr)
		return
	}
	if err == nil {
		compareBlocks(t, got, want)
	}
}

func compareBlocks(t *testing.T, got, want *uncoveredBlock) {
	t.Helper()
	if got == nil {
		if want != nil {
			t.Errorf("parseCoverageLine() returned nil, want %+v", want)
		}
		return
	}
	if got.File != want.File || got.Start != want.Start || got.End != want.End || got.Stmts != want.Stmts {
		t.Errorf("parseCoverageLine() = %+v, want %+v", got, want)
	}
}

func TestExtractFromLines(t *testing.T) {
	t.Parallel()
	lines := []string{"line1", "line2", "line3", "line4", "line5"}
	tests := []struct {
		name  string
		start int
		end   int
		want  string
	}{
		{
			name:  "middle range with context",
			start: 3,
			end:   4,
			want:  "line2\nline3\nline4",
		},
		{
			name:  "start range",
			start: 1,
			end:   2,
			want:  "line1\nline2",
		},
		{
			name:  "end range",
			start: 5,
			end:   5,
			want:  "line4\nline5",
		},
		{
			name:  "out of range",
			start: 10,
			end:   11,
			want:  "",
		},
		{
			name:  "empty lines",
			start: 1,
			end:   2,
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var l []string
			if tt.name != "empty lines" {
				l = lines
			}
			got := extractFromLines(l, tt.start, tt.end)
			if got != tt.want {
				t.Errorf("extractFromLines() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderReportSummary(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	catStats := map[string]int{
		"BUSINESS_LOGIC": 5,
		"ADAPTER":        2,
	}
	high := make([]uncoveredBlock, 3)
	medium := make([]uncoveredBlock, 4)
	lowCount := 1

	renderReportSummary(&sb, "./pkg", 8, high, medium, lowCount, catStats)

	got := sb.String()
	expected := []string{
		"Detailed Coverage Report for ./pkg",
		"- Total Gaps: 8",
		"- High Priority (Architectural): 3",
		"- Medium Priority (Technical Debt): 4",
		"- Low Priority: 1",
		"- ADAPTER: 2",
		"- BUSINESS_LOGIC: 5",
	}

	for _, want := range expected {
		if !strings.Contains(got, want) {
			t.Errorf("renderReportSummary() output does not contain %q\ngot: %s", want, got)
		}
	}
}

func TestAggregateCoverageStats(t *testing.T) {
	t.Parallel()
	blocks := []uncoveredBlock{
		{Priority: "High", Category: "ERROR_HANDLING"},
		{Priority: "High", Category: "BUSINESS_LOGIC"},
		{Priority: "Medium", Category: "BUSINESS_LOGIC"},
		{Priority: "Low", Category: "OTHER"},
	}

	high, medium, lowCount, catStats := aggregateCoverageStats(blocks)

	if len(high) != 2 {
		t.Errorf("expected 2 high priority blocks, got %d", len(high))
	}
	if len(medium) != 1 {
		t.Errorf("expected 1 medium priority blocks, got %d", len(medium))
	}
	if lowCount != 1 {
		t.Errorf("expected 1 low priority block, got %d", lowCount)
	}
	if catStats["BUSINESS_LOGIC"] != 2 {
		t.Errorf("expected 2 BUSINESS_LOGIC blocks, got %d", catStats["BUSINESS_LOGIC"])
	}
}

func TestRenderBlockGaps(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	blocks := []uncoveredBlock{
		{File: "file1.go", Start: 1, End: 10, Category: "CAT1", Code: "code1", Priority: "High"},
		{File: "file2.go", Start: 5, End: 15, Category: "CAT2", Code: "code2", Priority: "High"},
	}

	renderBlockGaps(&sb, "TEST PRIORITY", blocks, 1)

	got := sb.String()
	expected := []string{
		"[TEST PRIORITY]",
		"1. File: file1.go (Lines 1-10)",
		"Category: CAT1",
		"Code:\ncode1",
		"... and 1 more test priority gaps.",
	}

	for _, want := range expected {
		if !strings.Contains(got, want) {
			t.Errorf("renderBlockGaps() output does not contain %q\ngot: %s", want, got)
		}
	}

	if strings.Contains(got, "file2.go") {
		t.Error("renderBlockGaps() output should not contain file2.go due to maxItems=1")
	}
}

func TestParseLineNum(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		part    string
		want    int
		wantErr bool
	}{
		{"valid", "10.5", 10, false},
		{"valid single", "10", 10, false},
		{"invalid", "abc", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.part, func(t *testing.T) {
			t.Parallel()
			got, err := parseLineNum(tt.part)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseLineNum(%q) error = %v, wantErr %v", tt.part, err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("parseLineNum(%q) = %d, want %d", tt.part, got, tt.want)
			}
		})
	}
}

func TestParseSymbolLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		pathAndRange string
		prefix       string
		wantFile     string
		wantStart    int
		wantEnd      int
		wantErr      bool
	}{
		{"valid with prefix", "github.com/user/repo/file.go:1,2", "github.com/user/repo/", "file.go", 1, 2, false},
		{"invalid format", "no-colon", "", "", 0, 0, true},
		{"invalid range", "file.go:1", "", "", 0, 0, true},
		{"invalid start", "file.go:abc,2", "", "", 0, 0, true},
		{"invalid end", "file.go:1,abc", "", "", 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseSymbolLine(tt.pathAndRange, tt.prefix)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSymbolLine(%q) error = %v, wantErr %v", tt.pathAndRange, err, tt.wantErr)
			}
			if err == nil {
				if got.File != tt.wantFile || got.Start != tt.wantStart || got.End != tt.wantEnd {
					t.Errorf("parseSymbolLine() = %+v, want file=%s, start=%d, end=%d", got, tt.wantFile, tt.wantStart, tt.wantEnd)
				}
			}
		})
	}
}

func TestParseCoverageProfile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockExecutor{
		OutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("github.com/user/repo"), nil
		},
	}

	content := "mode: set\ngithub.com/user/repo/file.go:1.0,2.0 1 0\n"
	r := strings.NewReader(content)

	blocks, err := parseCoverageProfile(ctx, r, mock)
	if err != nil {
		t.Fatalf("parseCoverageProfile failed: %v", err)
	}

	if len(blocks) != 1 {
		t.Errorf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].File != "file.go" {
		t.Errorf("expected file.go, got %s", blocks[0].File)
	}
}

func TestGetDetailedCoverageReport(t *testing.T) {
	t.Parallel()
	// This test is harder because it runs 'go test' and reads from FS.
	// We can try to mock but getDetailedCoverage creates temp files and runs actual commands.

	// Let's test with a mock that fails to run go test
	ctx := context.Background()
	mock := &mockExecutor{
		CombinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return nil, os.ErrNotExist
		},
	}

	_, err := getDetailedCoverageReport(ctx, "./non-existent", mock)
	if err == nil {
		t.Error("expected error for non-existent package, got nil")
	}
}

func TestGetDetailedCoverageJSON(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockExecutor{
		CombinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return nil, os.ErrNotExist
		},
	}

	_, err := getDetailedCoverageJSON(ctx, "./non-existent", "High", mock)
	if err == nil {
		t.Error("expected error for non-existent package, got nil")
	}
}

func TestGetModuleName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		mock := &mockExecutor{
			OutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				return []byte("github.com/test/mod\n"), nil
			},
		}
		mod := getModuleName(ctx, mock)
		if mod != "github.com/test/mod/" {
			t.Errorf("expected github.com/test/mod/, got %q", mod)
		}
	})

	t.Run("failure", func(t *testing.T) {
		t.Parallel()
		mock := &mockExecutor{
			OutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				return nil, os.ErrNotExist
			},
		}
		mod := getModuleName(ctx, mock)
		if mod != "" {
			t.Errorf("expected empty string, got %q", mod)
		}
	})
}

func TestParseDetailedCoverage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockExecutor{
		OutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("github.com/user/repo"), nil
		},
	}

	content := "mode: set\ngithub.com/user/repo/file.go:1.0,2.0 1 0\n"
	r := strings.NewReader(content)

	readFile := func(path string) ([]byte, error) {
		if path == "file.go" {
			return []byte("line1\nline2\nline3\n"), nil
		}
		return nil, os.ErrNotExist
	}

	blocks, err := parseDetailedCoverage(ctx, r, mock, readFile)
	if err != nil {
		t.Fatalf("parseDetailedCoverage failed: %v", err)
	}

	if len(blocks) != 1 {
		t.Errorf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Code != "line1\nline2" {
		t.Errorf("expected code line1\nline2, got %q", blocks[0].Code)
	}
}

func TestFormatDetailedCoverageReport(t *testing.T) {
	t.Parallel()
	blocks := []uncoveredBlock{
		{File: "file1.go", Start: 1, End: 2, Category: "BUSINESS_LOGIC", Priority: "High", Code: "code1"},
	}
	report := formatDetailedCoverageReport("./pkg", blocks)
	if !strings.Contains(report, "Detailed Coverage Report for ./pkg") {
		t.Error("report missing title")
	}
	if !strings.Contains(report, "HIGH PRIORITY GAPS") {
		t.Error("report missing high priority section")
	}
}

func TestFormatDetailedCoverageJSON(t *testing.T) {
	t.Parallel()
	blocks := []uncoveredBlock{
		{File: "file1.go", Start: 1, End: 2, Category: "BUSINESS_LOGIC", Priority: "High", Code: "code1"},
	}
	jsonStr, err := formatDetailedCoverageJSON(blocks, "High")
	if err != nil {
		t.Fatalf("formatDetailedCoverageJSON failed: %v", err)
	}
	if !strings.Contains(jsonStr, "file1.go") {
		t.Error("json missing file name")
	}

	jsonStr, err = formatDetailedCoverageJSON(blocks, "Low")
	if err != nil {
		t.Fatalf("formatDetailedCoverageJSON failed: %v", err)
	}
	if !strings.Contains(jsonStr, "file1.go") {
		t.Error("json missing file name for Low priority filter")
	}
}

func TestParseDetailedCoverage_FileReadError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockExecutor{
		OutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("github.com/user/repo"), nil
		},
	}

	content := "mode: set\ngithub.com/user/repo/file.go:1.0,2.0 1 0\n"
	r := strings.NewReader(content)

	readFile := func(path string) ([]byte, error) {
		return nil, fmt.Errorf("read error")
	}

	blocks, err := parseDetailedCoverage(ctx, r, mock, readFile)
	if err != nil {
		t.Fatalf("parseDetailedCoverage failed: %v", err)
	}

	if len(blocks) != 1 {
		t.Errorf("expected 1 block, got %d", len(blocks))
	}
	if !strings.Contains(blocks[0].Code, "Error reading file file.go") {
		t.Errorf("expected error message in code, got %q", blocks[0].Code)
	}
}

func TestGetDetailedCoverage_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// We need to create a real temp file for the mock to "generate"
	f, _ := os.CreateTemp("", "test-coverage-*.out")
	tempPath := f.Name()
	defer os.Remove(tempPath)
	if _, err := f.WriteString("mode: set\ngithub.com/user/repo/file.go:1.0,2.0 1 0\n"); err != nil {
		t.Fatalf("failed to write coverage string: %v", err)
	}
	f.Close()

	mock := &mockExecutor{
		OutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("github.com/user/repo"), nil
		},
		CombinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			// Mocking go test: we need to find the -coverprofile= part and write to it
			for _, arg := range args {
				if strings.HasPrefix(arg, "-coverprofile=") {
					path := strings.TrimPrefix(arg, "-coverprofile=")
					if err := os.WriteFile(path, []byte("mode: set\ngithub.com/user/repo/file.go:1.0,2.0 1 0\n"), 0644); err != nil {
						t.Errorf("failed to write mock coverage file: %v", err)
					}
				}
			}
			return []byte("ok"), nil
		},
	}

	// Mock os.ReadFile by overriding the internal helper if we had one,
	// but getDetailedCoverage uses os.ReadFile directly.
	// So we need to create a real file on disk for file.go or it will have an error message in Code.
	if err := os.WriteFile("file.go", []byte("package main\nfunc main() {}\n"), 0644); err != nil {
		t.Fatalf("failed to write mock go file: %v", err)
	}
	defer os.Remove("file.go")

	blocks, err := getDetailedCoverage(ctx, ".", mock)
	if err != nil {
		t.Fatalf("getDetailedCoverage failed: %v", err)
	}

	if len(blocks) != 1 {
		t.Errorf("expected 1 block, got %d", len(blocks))
	}
}

func TestFormatDetailedCoverageReport_MoreOptions(t *testing.T) {
	t.Parallel()
	// Test few high priority gaps and some medium

	blocks := []uncoveredBlock{
		{File: "f1.go", Priority: "High", Category: "BUS"},
		{File: "f2.go", Priority: "Medium", Category: "ADAP"},
	}
	report := formatDetailedCoverageReport("pkg", blocks)
	if !strings.Contains(report, "MEDIUM PRIORITY GAPS") {
		t.Error("expected medium priority gaps to be shown")
	}

	// Test many gaps for "and X more"
	blocks = nil
	for i := 0; i < 15; i++ {
		blocks = append(blocks, uncoveredBlock{File: fmt.Sprintf("f%d.go", i), Priority: "High", Category: "BUS"})
	}
	report = formatDetailedCoverageReport("pkg", blocks)
	if !strings.Contains(report, "and 5 more high priority gaps") {
		t.Error("expected 'and X more' message")
	}
}

func TestGetDetailedCoverage_EmptyProfile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockExecutor{
		CombinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			for _, arg := range args {
				if strings.HasPrefix(arg, "-coverprofile=") {
					path := strings.TrimPrefix(arg, "-coverprofile=")
					if err := os.WriteFile(path, []byte(""), 0644); err != nil {
						t.Errorf("failed to write empty file: %v", err)
					} // Empty file
				}
			}
			return []byte("ok"), nil
		},
	}

	_, err := getDetailedCoverage(ctx, ".", mock)
	if err == nil || !strings.Contains(err.Error(), "coverage profile is empty") {
		t.Errorf("expected empty profile error, got %v", err)
	}
}

func TestParseCoverageProfile_MalformedLine(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockExecutor{
		OutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("github.com/user/repo"), nil
		},
	}

	content := "mode: set\nmalformed line\ngithub.com/user/repo/file.go:1.0,2.0 1 0\n"
	r := strings.NewReader(content)

	blocks, err := parseCoverageProfile(ctx, r, mock)
	if err != nil {
		t.Fatalf("parseCoverageProfile failed: %v", err)
	}

	if len(blocks) != 1 {
		t.Errorf("expected 1 valid block, got %d", len(blocks))
	}
}

func TestGetDetailedCoverageReport_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockExecutor{
		OutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("github.com/user/repo"), nil
		},
		CombinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			for _, arg := range args {
				if strings.HasPrefix(arg, "-coverprofile=") {
					path := strings.TrimPrefix(arg, "-coverprofile=")
					if err := os.WriteFile(path, []byte("mode: set\ngithub.com/user/repo/file.go:1.0,2.0 1 0\n"), 0644); err != nil {
						t.Errorf("failed to write mock coverage file: %v", err)
					}
				}
			}
			return []byte("ok"), nil
		},
	}
	if err := os.WriteFile("file.go", []byte("package main\nfunc main() {}\n"), 0644); err != nil {
		t.Fatalf("failed to write mock go file: %v", err)
	}
	defer os.Remove("file.go")

	report, err := getDetailedCoverageReport(ctx, ".", mock)
	if err != nil {
		t.Fatalf("getDetailedCoverageReport failed: %v", err)
	}
	if !strings.Contains(report, "Detailed Coverage Report") {
		t.Error("report missing title")
	}
}

func TestGetDetailedCoverageJSON_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockExecutor{
		OutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("github.com/user/repo"), nil
		},
		CombinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			for _, arg := range args {
				if strings.HasPrefix(arg, "-coverprofile=") {
					path := strings.TrimPrefix(arg, "-coverprofile=")
					if err := os.WriteFile(path, []byte("mode: set\ngithub.com/user/repo/file.go:1.0,2.0 1 0\n"), 0644); err != nil {
						t.Errorf("failed to write mock coverage file: %v", err)
					}
				}
			}
			return []byte("ok"), nil
		},
	}
	if err := os.WriteFile("file.go", []byte("package main\nfunc main() {}\n"), 0644); err != nil {
		t.Fatalf("failed to write mock go file: %v", err)
	}
	defer os.Remove("file.go")

	jsonStr, err := getDetailedCoverageJSON(ctx, ".", "Low", mock)
	if err != nil {
		t.Fatalf("getDetailedCoverageJSON failed: %v", err)
	}
	if !strings.Contains(jsonStr, "file.go") {
		t.Error("json missing file name")
	}
}

func TestExtractFromLines_EdgeCases(t *testing.T) {
	t.Parallel()
	lines := []string{"line1", "line2"}
	// Test start = 0
	got := extractFromLines(lines, 0, 1)
	if got != "line1" {
		t.Errorf("expected line1, got %q", got)
	}

	// Test end > len(lines)
	got = extractFromLines(lines, 1, 5)
	if got != "line1\nline2" {
		t.Errorf("expected line1\\nline2, got %q", got)
	}

	// Test startIdx >= endIdx
	got = extractFromLines(lines, 5, 2)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestParseCoverageProfile_Empty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockExecutor{}
	r := strings.NewReader("")
	blocks, err := parseCoverageProfile(ctx, r, mock)
	if err != nil {
		t.Errorf("expected nil error for empty reader, got %v", err)
	}
	if len(blocks) != 0 {
		t.Errorf("expected 0 blocks, got %d", len(blocks))
	}
}

func TestFormatDetailedCoverageReport_NoGaps(t *testing.T) {
	t.Parallel()
	report := formatDetailedCoverageReport("pkg", nil)
	if !strings.Contains(report, "Total Gaps: 0") {
		t.Error("report should indicate 0 gaps")
	}
	if strings.Contains(report, "HIGH PRIORITY GAPS") {
		t.Error("report should not contain gaps section")
	}
}

type errorReader struct{}

func (e *errorReader) Read(p []byte) (n int, err error) {
	return 0, fmt.Errorf("read error")
}

func TestParseCoverageProfile_ScannerError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockExecutor{}
	r := &errorReader{}
	_, err := parseCoverageProfile(ctx, r, mock)
	if err == nil || !strings.Contains(err.Error(), "read error") {
		t.Errorf("expected read error, got %v", err)
	}
}

func TestParseCoverageLine_SpecialLines(t *testing.T) {
	t.Parallel()
	// Empty line

	got, err := parseCoverageLine("", "")
	if got != nil || err != nil {
		t.Error("expected nil, nil for empty line")
	}

	// Mode line
	got, err = parseCoverageLine("mode: set", "")
	if got != nil || err != nil {
		t.Error("expected nil, nil for mode line")
	}
}

func TestFormatDetailedCoverageReport_MediumSlots(t *testing.T) {
	t.Parallel()
	// len(high) < 5

	var high []uncoveredBlock
	for i := 0; i < 4; i++ {
		high = append(high, uncoveredBlock{Priority: "High"})
	}
	medium := []uncoveredBlock{{Priority: "Medium"}}

	report := formatDetailedCoverageReport("pkg", append(high, medium...))
	if !strings.Contains(report, "MEDIUM PRIORITY GAPS") {
		t.Error("medium gaps should be shown if high gaps are few")
	}
}

type delayedErrorReader struct {
	content string
	offset  int
}

func (d *delayedErrorReader) Read(p []byte) (n int, err error) {
	if d.offset >= len(d.content) {
		return 0, fmt.Errorf("delayed read error")
	}
	n = copy(p, d.content[d.offset:])
	d.offset += n
	return n, nil
}

func TestParseDetailedCoverage_ProfileError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockExecutor{}
	// Valid first line, then error
	r := &delayedErrorReader{content: "mode: set\n"}

	_, err := parseDetailedCoverage(ctx, r, mock, os.ReadFile)
	if err == nil || !strings.Contains(err.Error(), "delayed read error") {
		t.Errorf("expected delayed read error, got %v", err)
	}
}

func TestGetModuleName_WithTrailingSlash(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockExecutor{
		OutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("github.com/test/mod/\n"), nil
		},
	}
	mod := getModuleName(ctx, mock)
	if mod != "github.com/test/mod/" {
		t.Errorf("expected github.com/test/mod/, got %q", mod)
	}
}

func TestGetDetailedCoverage_CreateTempError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockExecutor{}

	// Set TMPDIR to a non-existent directory to force os.CreateTemp to fail
	oldTmp := os.Getenv("TMPDIR")
	os.Setenv("TMPDIR", "/non-existent-directory-12345")
	defer os.Setenv("TMPDIR", oldTmp)

	_, err := getDetailedCoverage(ctx, ".", mock)
	if err == nil {
		t.Error("expected error from os.CreateTemp, got nil")
	}
}
