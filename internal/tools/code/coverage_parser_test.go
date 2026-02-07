package code

import (
	"strings"
	"testing"
)

func TestUncoveredBlock_Classify(t *testing.T) {
	tests := []struct {
		name     string
		block    UncoveredBlock
		wantCat  string
		wantPrio string
	}{
		{
			name: "error handling high priority",
			block: UncoveredBlock{
				File: "internal/domain/service.go",
				Code: "if err != nil { return err }",
			},
			wantCat:  "ERROR_HANDLING",
			wantPrio: "High",
		},
		{
			name: "business logic medium priority",
			block: UncoveredBlock{
				File: "internal/service/logic.go",
				Code: "x := y + 1",
			},
			wantCat:  "BUSINESS_LOGIC",
			wantPrio: "Medium",
		},
		{
			name: "adapter low priority",
			block: UncoveredBlock{
				File: "internal/api/handler.go",
				Code: "w.WriteHeader(200)",
			},
			wantCat:  "ADAPTER",
			wantPrio: "Medium", // Adapter + !isErrorHandling -> Medium
		},
		{
			name: "other low priority",
			block: UncoveredBlock{
				File: "main.go",
				Code: "fmt.Println()",
			},
			wantCat:  "OTHER",
			wantPrio: "Low",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
	tests := []struct {
		name    string
		line    string
		prefix  string
		want    *UncoveredBlock
		wantOk  bool
	}{
		{
			name:   "uncovered line",
			line:   "github.com/user/repo/pkg/file.go:10.5,12.10 3 0",
			prefix: "github.com/user/repo/",
			want: &UncoveredBlock{
				File:  "pkg/file.go",
				Start: 10,
				End:   12,
				Stmts: 3,
			},
			wantOk: true,
		},
		{
			name:   "covered line (skipped)",
			line:   "github.com/user/repo/pkg/file.go:10.5,12.10 3 1",
			prefix: "github.com/user/repo/",
			want:   nil,
			wantOk: false,
		},
		{
			name:   "invalid line",
			line:   "invalid",
			prefix: "",
			want:   nil,
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseCoverageLine(tt.line, tt.prefix)
			if ok != tt.wantOk {
				t.Errorf("parseCoverageLine() ok = %v, want %v", ok, tt.wantOk)
			}
			if ok && (got.File != tt.want.File || got.Start != tt.want.Start || got.End != tt.want.End || got.Stmts != tt.want.Stmts) {
				t.Errorf("parseCoverageLine() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestExtractFromLines(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFromLines(lines, tt.start, tt.end)
			if got != tt.want {
				t.Errorf("extractFromLines() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderReportSummary(t *testing.T) {
	var sb strings.Builder
	catStats := map[string]int{
		"BUSINESS_LOGIC": 5,
		"ADAPTER":        2,
	}
	high := make([]UncoveredBlock, 3)
	medium := make([]UncoveredBlock, 4)
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
	blocks := []UncoveredBlock{
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
	var sb strings.Builder
	blocks := []UncoveredBlock{
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
	tests := []struct {
		name string
		part string
		want int
		ok   bool
	}{
		{"valid", "10.5", 10, true},
		{"valid single", "10", 10, true},
		{"invalid", "abc", 0, false},
		{"empty", "", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseLineNum(tt.part)
			if ok != tt.ok {
				t.Errorf("parseLineNum(%q) ok = %v, want %v", tt.part, ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("parseLineNum(%q) = %d, want %d", tt.part, got, tt.want)
			}
		})
	}
}

func TestParsePathAndRange(t *testing.T) {
	tests := []struct {
		name         string
		pathAndRange string
		prefix       string
		wantFile     string
		wantStart    int
		wantEnd      int
		ok           bool
	}{
		{"valid with prefix", "github.com/user/repo/file.go:1,2", "github.com/user/repo/", "file.go", 1, 2, true},
		{"valid no prefix", "file.go:10,20", "", "file.go", 10, 20, true},
		{"invalid format", "file.go", "", "", 0, 0, false},
		{"invalid range", "file.go:1", "", "", 0, 0, false},
		{"invalid line", "file.go:a,b", "", "", 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parsePathAndRange(tt.pathAndRange, tt.prefix)
			if ok != tt.ok {
				t.Errorf("parsePathAndRange(%q) ok = %v, want %v", tt.pathAndRange, ok, tt.ok)
			}
			if ok {
				if got.File != tt.wantFile || got.Start != tt.wantStart || got.End != tt.wantEnd {
					t.Errorf("parsePathAndRange() = %+v, want file=%s, start=%d, end=%d", got, tt.wantFile, tt.wantStart, tt.wantEnd)
				}
			}
		})
	}
}
