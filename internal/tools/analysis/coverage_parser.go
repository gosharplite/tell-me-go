// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

// uncoveredBlock represents a block of code with zero coverage.
type uncoveredBlock struct {
	File     string `json:"file"`
	Start    int    `json:"start"`
	End      int    `json:"end"`
	Stmts    int    `json:"stmts"`
	Code     string `json:"code,omitempty"`
	Category string `json:"category"`
	Priority string `json:"priority"`
	// CatalogTitle names the ACCEPTED entry in the Intentional Non-Fixes
	// catalog whose references cover this block's range, when one exists.
	// A non-empty value marks the gap as deliberately accepted (not actionable).
	CatalogTitle string `json:"catalog_title,omitempty"`
}

// classificationRule defines a rule for categorizing uncovered code blocks.
type classificationRule interface {
	category() string
	match(b *uncoveredBlock) bool
}

type errorHandlingRule struct{}

func (r errorHandlingRule) category() string { return "ERROR_HANDLING" }
func (r errorHandlingRule) match(b *uncoveredBlock) bool {
	lowerCode := strings.ToLower(b.Code)
	return strings.Contains(lowerCode, "if err != nil") ||
		(strings.Contains(lowerCode, "return") && strings.Contains(lowerCode, "err")) ||
		strings.Contains(lowerCode, "fmt.errorf") ||
		strings.Contains(lowerCode, "errors.new")
}

type businessLogicRule struct{}

func (r businessLogicRule) category() string { return "BUSINESS_LOGIC" }
func (r businessLogicRule) match(b *uncoveredBlock) bool {
	paths := []string{"internal/domain", "internal/usecase", "internal/agent", "internal/service"}
	for _, p := range paths {
		if strings.HasPrefix(b.File, p+"/") || b.File == p {
			return true
		}
	}
	return false
}

type adapterRule struct{}

func (r adapterRule) category() string { return "ADAPTER" }
func (r adapterRule) match(b *uncoveredBlock) bool {
	paths := []string{"internal/repository", "internal/gateway", "internal/transport", "internal/api", "internal/auth", "internal/infrastructure"}
	for _, p := range paths {
		if strings.HasPrefix(b.File, p+"/") || b.File == p {
			return true
		}
	}
	return false
}

var rules = []classificationRule{
	errorHandlingRule{},
	businessLogicRule{},
	adapterRule{},
}

// jsonMarshalIndent is the function used to marshal JSON. Exposed as a variable
// to allow test injection of marshal failures for error-path coverage.
var jsonMarshalIndent = json.MarshalIndent

// osOpenFile is the function used to open coverage profile files. Exposed as a
// variable to allow test injection of file-open failures for error-path coverage.
var osOpenFile = os.Open

// Classify categorizes the block and assigns a priority based on heuristics.
func (b *uncoveredBlock) Classify() {
	b.Category = "OTHER"
	for _, rule := range rules {
		if rule.match(b) {
			b.Category = rule.category()
			break
		}
	}

	b.Priority = b.determinePriority()
}

func (b *uncoveredBlock) determinePriority() string {
	isErr := (errorHandlingRule{}).match(b)
	isBiz := (businessLogicRule{}).match(b)
	isAdap := (adapterRule{}).match(b)

	if isErr && isBiz {
		return "High"
	}
	if isErr || isBiz || isAdap {
		return "Medium"
	}
	return "Low"
}

// extractFromLines extracts a range of lines from a slice, including one line of context before.
func extractFromLines(lines []string, start, end int) string {
	if len(lines) == 0 {
		return ""
	}

	startWithContext := start
	if startWithContext > 1 {
		startWithContext--
	}

	startIdx := startWithContext - 1
	endIdx := end

	if startIdx < 0 {
		startIdx = 0
	}
	if endIdx > len(lines) {
		endIdx = len(lines)
	}
	if startIdx >= endIdx {
		return ""
	}

	return strings.Join(lines[startIdx:endIdx], "\n")
}

// getModuleName returns the module name from the environment.
func getModuleName(ctx context.Context, runner AnalysisGoRunner) string {
	mod, err := runner.GetModulePath(ctx)
	if err != nil {
		return ""
	}
	if mod != "" && !strings.HasSuffix(mod, "/") {
		mod += "/"
	}
	return mod
}

func parseLineNum(part string) (int, error) {
	subParts := strings.Split(part, ".")
	val, err := strconv.Atoi(subParts[0])
	if err != nil {
		return 0, fmt.Errorf("not a number: %w", err)
	}
	return val, nil
}

func parseFile(pathAndRange string, modulePrefix string) (string, string) {
	colonIdx := strings.LastIndex(pathAndRange, ":")
	if colonIdx == -1 {
		return pathAndRange, ""
	}

	file := pathAndRange[:colonIdx]
	if modulePrefix != "" && strings.HasPrefix(file, modulePrefix) {
		file = file[len(modulePrefix):]
	}
	return file, pathAndRange[colonIdx+1:]
}

func parseRange(rangePart string) (int, int, error) {
	rangeParts := strings.Split(rangePart, ",")
	if len(rangeParts) != 2 {
		return 0, 0, fmt.Errorf("invalid range format: %s", rangePart)
	}

	startLine, err1 := parseLineNum(rangeParts[0])
	if err1 != nil {
		return 0, 0, fmt.Errorf("invalid start line: %w", err1)
	}
	endLine, err2 := parseLineNum(rangeParts[1])
	if err2 != nil {
		return 0, 0, fmt.Errorf("invalid end line: %w", err2)
	}
	return startLine, endLine, nil
}

func parseSymbolLine(pathAndRange string, modulePrefix string) (*uncoveredBlock, error) {
	file, rangePart := parseFile(pathAndRange, modulePrefix)
	if rangePart == "" {
		return nil, fmt.Errorf("missing colon in path: %s", pathAndRange)
	}

	startLine, endLine, err := parseRange(rangePart)
	if err != nil {
		return nil, err
	}

	return &uncoveredBlock{
		File:  file,
		Start: startLine,
		End:   endLine,
	}, nil
}

func validateLine(line string) ([]string, error) {
	parts := strings.Fields(line)
	if len(parts) != 3 {
		return nil, fmt.Errorf("expected 3 fields, got %d", len(parts))
	}
	return parts, nil
}

func parseProfileLine(parts []string) (int, error) {
	count, err := strconv.Atoi(parts[2])
	if err != nil {
		return 0, fmt.Errorf("invalid count: %w", err)
	}
	return count, nil
}

func isDataLine(line string) bool {
	return line != "" && !strings.HasPrefix(line, "mode:")
}

func parseCoverageLine(line string, modulePrefix string) (*uncoveredBlock, error) {
	if !isDataLine(line) {
		return nil, nil
	}

	parts, err := validateLine(line)
	if err != nil {
		return nil, err
	}

	return parseDataParts(parts, modulePrefix)
}

func parseDataParts(parts []string, modulePrefix string) (*uncoveredBlock, error) {
	count, err := parseProfileLine(parts)
	if err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, nil
	}

	stmts, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid stmts: %w", err)
	}

	block, err := parseSymbolLine(parts[0], modulePrefix)
	if err != nil {
		return nil, err
	}
	block.Stmts = stmts
	return block, nil
}

// parseCoverageProfile parses a go coverage profile and returns blocks with zero coverage.
func parseCoverageProfile(ctx context.Context, r io.Reader, runner AnalysisGoRunner) ([]uncoveredBlock, error) {
	var blocks []uncoveredBlock
	scanner := bufio.NewScanner(r)
	modulePrefix := getModuleName(ctx, runner)

	if !scanner.Scan() {
		return nil, scanner.Err()
	}

	for scanner.Scan() {
		block, err := parseCoverageLine(scanner.Text(), modulePrefix)
		if err != nil {
			continue
		}
		if block != nil {
			blocks = append(blocks, *block)
		}
	}

	return blocks, scanner.Err()
}

// parseDetailedCoverage parses a coverage profile from a reader and fetches code for each block.
func parseDetailedCoverage(ctx context.Context, r io.Reader, runner AnalysisGoRunner, readFile func(string) ([]byte, error)) ([]uncoveredBlock, error) {
	blocks, err := parseCoverageProfile(ctx, r, runner)
	if err != nil {
		return nil, err
	}

	var (
		fileCache = make(map[string][]string)
		mu        sync.RWMutex
		wg        sync.WaitGroup
	)

	for i := range blocks {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			mu.RLock()
			lines, ok := fileCache[blocks[idx].File]
			mu.RUnlock()

			if !ok {
				mu.Lock()
				// Double-check after lock
				lines, ok = fileCache[blocks[idx].File]
				if !ok {
					content, err := readFile(blocks[idx].File)
					if err != nil {
						blocks[idx].Code = fmt.Sprintf("[Error reading file %s: %v]", blocks[idx].File, err)
						blocks[idx].Classify()
						mu.Unlock()
						return
					}
					lines = strings.Split(string(content), "\n")
					fileCache[blocks[idx].File] = lines
				}
				mu.Unlock()
			}

			blocks[idx].Code = extractFromLines(lines, blocks[idx].Start, blocks[idx].End)
			blocks[idx].Classify()
		}(i)
	}

	wg.Wait()
	return blocks, nil
}

// runCoverageTest executes the coverage test with a heartbeat.
func (m *healthManager) runCoverageTest(ctx context.Context, packagePath, tempPath string, hb chan<- struct{}) error {
	// Heartbeat while running tests
	done := make(chan struct{})
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		clk := m.clk
		if clk == nil {
			clk = clock.RealClock{}
		}
		ticker := clk.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C():
				if hb != nil {
					select {
					case hb <- struct{}{}:
					default:
					}
				}
			}
		}
	}()
	defer func() {
		close(done)
		<-exited
	}()

	_, err := m.Runner.RunTestsWithCoverage(ctx, packagePath, true, tempPath)
	return err
}

// validateProfile checks if the coverage profile was correctly generated.
func validateProfile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("coverage profile was not generated: %w", err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("coverage profile is empty; check if package path is valid and contains testable Go files")
	}
	return nil
}

// getDetailedCoverage executes the coverage test and parses the profile.
func (m *healthManager) getDetailedCoverage(ctx context.Context, packagePath string, hb chan<- struct{}) ([]uncoveredBlock, error) {
	f, err := os.CreateTemp("", "coverage-*.out")
	if err != nil {
		return nil, err
	}
	tempPath := f.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()
	_ = f.Close()

	testErr := m.runCoverageTest(ctx, packagePath, tempPath, hb)

	if err := validateProfile(tempPath); err != nil {
		if testErr != nil {
			return nil, fmt.Errorf("test execution failed and no valid profile was generated: %w", testErr)
		}
		return nil, err
	}

	cf, err := osOpenFile(tempPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = cf.Close()
	}()

	blocks, err := parseDetailedCoverage(ctx, cf, m.Runner, os.ReadFile)
	if err != nil {
		return nil, err
	}

	if testErr != nil {
		return blocks, fmt.Errorf("coverage error (profile may be incomplete): %w", testErr)
	}
	return blocks, nil
}

// filterExcludedBlocks removes blocks whose File path contains any of the
// excluded package patterns. This matches the Makefile's grep -v exclusion
// for *test packages (mocks, stubs, test doubles) whose error-return paths
// are exercised via m.Called() — opaque to static coverage analysis.
// When excluded is empty or nil, the original slice is returned unchanged.
func filterExcludedBlocks(blocks []uncoveredBlock, excluded []string) []uncoveredBlock {
	if len(excluded) == 0 {
		return blocks
	}
	filtered := make([]uncoveredBlock, 0, len(blocks))
	for _, b := range blocks {
		skip := false
		for _, pattern := range excluded {
			if strings.Contains(b.File, pattern) {
				skip = true
				break
			}
		}
		if !skip {
			filtered = append(filtered, b)
		}
	}
	return filtered
}

// applyCatalogTitles cross-references uncovered blocks against the given
// Intentional Non-Fixes catalog entries, tagging each block whose range falls
// inside an ACCEPTED entry with that entry's title. Cataloged blocks keep
// their original Category/Priority (set by Classify); reporting layers decide
// how to surface them. A nil or empty entry slice is a no-op.
func applyCatalogTitles(blocks []uncoveredBlock, entries []nonFixEntry) {
	for i := range blocks {
		blocks[i].CatalogTitle = catalogTitleForRange(entries, blocks[i].File, blocks[i].Start, blocks[i].End)
	}
}

// getDetailedCoverageReport generates a formatted report optimized for LLM consumption.
func (m *healthManager) getDetailedCoverageReport(ctx context.Context, packagePath string, excludedPackages []string, hb chan<- struct{}) (string, error) {
	blocks, err := m.getDetailedCoverage(ctx, packagePath, hb)
	if err != nil && len(blocks) == 0 {
		return "", err
	}

	if len(excludedPackages) > 0 {
		blocks = filterExcludedBlocks(blocks, excludedPackages)
	}

	// Load the catalog once and tag ACCEPTED gaps after Classify() so
	// Priority/Category for uncataloged blocks remain exactly as before. A
	// load failure degrades gracefully: all gaps stay actionable.
	entries := m.loadNonFixEntries()
	applyCatalogTitles(blocks, entries)

	report := formatDetailedCoverageReport(packagePath, blocks)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			report = fmt.Sprintf("⚠️ NOTE: coverage generation interrupted; profile may be incomplete.\nCause: %v\n\n%s", err, report)
		} else {
			report = fmt.Sprintf("⚠️ WARNING: coverage error; profile may be incomplete.\nCause: %v\n\n%s", err, report)
		}
	}

	return report, nil
}

func formatDetailedCoverageReport(packagePath string, blocks []uncoveredBlock) string {
	high, medium, lowCount, cataloged, catStats := aggregateCoverageStats(blocks)

	var sb strings.Builder
	renderReportSummary(&sb, packagePath, len(blocks), high, medium, lowCount, len(cataloged), catStats)

	const maxItems = 10
	renderBlockGaps(&sb, "HIGH PRIORITY GAPS", high, maxItems)

	if len(medium) > 0 && len(high) < 5 {
		remainingSlots := maxItems - len(high)
		renderBlockGaps(&sb, "MEDIUM PRIORITY GAPS", medium, remainingSlots)
	}

	renderCatalogedGaps(&sb, cataloged, maxItems)

	return sb.String()
}

func aggregateCoverageStats(blocks []uncoveredBlock) (high []uncoveredBlock, medium []uncoveredBlock, lowCount int, cataloged []uncoveredBlock, catStats map[string]int) {
	catStats = make(map[string]int)
	for _, b := range blocks {
		catStats[b.Category]++
		// Blocks covered by an ACCEPTED catalog entry are already-accepted
		// gaps: they are excluded from the actionable priority buckets.
		if b.CatalogTitle != "" {
			cataloged = append(cataloged, b)
			continue
		}
		switch b.Priority {
		case "High":
			high = append(high, b)
		case "Medium":
			medium = append(medium, b)
		case "Low":
			lowCount++
		}
	}
	return
}

func renderReportSummary(sb *strings.Builder, packagePath string, total int, high, medium []uncoveredBlock, lowCount, catalogedCount int, catStats map[string]int) {
	_, _ = fmt.Fprintf(sb, "Detailed Coverage Report for %s\n", packagePath)
	sb.WriteString(strings.Repeat("-", len(packagePath)+29) + "\n")
	sb.WriteString("Summary:\n")
	_, _ = fmt.Fprintf(sb, "- Total Gaps: %d\n", total)
	_, _ = fmt.Fprintf(sb, "- High Priority (Architectural): %d\n", len(high))
	_, _ = fmt.Fprintf(sb, "- Medium Priority (Technical Debt): %d\n", len(medium))
	_, _ = fmt.Fprintf(sb, "- Low Priority: %d\n", lowCount)
	_, _ = fmt.Fprintf(sb, "- Cataloged (ACCEPTED): %d\n", catalogedCount)
	sb.WriteString("\nBreakdown by Category:\n")

	var cats []string
	for c := range catStats {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	for _, cat := range cats {
		_, _ = fmt.Fprintf(sb, "- %s: %d\n", cat, catStats[cat])
	}
}

func renderBlockGaps(sb *strings.Builder, title string, blocks []uncoveredBlock, maxItems int) {
	if len(blocks) == 0 {
		return
	}
	_, _ = fmt.Fprintf(sb, "\n[%s]\n", title)

	label := strings.ToLower(title)
	label = strings.TrimSuffix(label, " gaps")

	for i, b := range blocks {
		if i >= maxItems {
			_, _ = fmt.Fprintf(sb, "... and %d more %s gaps.\n", len(blocks)-maxItems, label)
			break
		}
		_, _ = fmt.Fprintf(sb, "%d. File: %s (Lines %d-%d)\n", i+1, b.File, b.Start, b.End)
		_, _ = fmt.Fprintf(sb, "   Category: %s\n", b.Category)
		_, _ = fmt.Fprintf(sb, "   Code:\n%s\n\n", b.Code)
	}
}

// renderCatalogedGaps lists blocks whose ranges fall inside an ACCEPTED entry
// of the Intentional Non-Fixes catalog, so readers can verify the acceptance
// rationale before treating them as actionable gaps.
func renderCatalogedGaps(sb *strings.Builder, blocks []uncoveredBlock, maxItems int) {
	if len(blocks) == 0 {
		return
	}
	_, _ = fmt.Fprintf(sb, "\n[CATALOGED GAPS (ACCEPTED)]\n")

	for i, b := range blocks {
		if i >= maxItems {
			_, _ = fmt.Fprintf(sb, "... and %d more cataloged (ACCEPTED) gaps.\n", len(blocks)-maxItems)
			break
		}
		_, _ = fmt.Fprintf(sb, "%d. File: %s (Lines %d-%d)\n", i+1, b.File, b.Start, b.End)
		_, _ = fmt.Fprintf(sb, "   Category: %s\n", b.Category)
		_, _ = fmt.Fprintf(sb, "   Catalog: %s\n", b.CatalogTitle)
	}
}

// getDetailedCoverageJSON returns the uncovered blocks as a JSON string, filtered by priority.
func (m *healthManager) getDetailedCoverageJSON(ctx context.Context, packagePath string, minPriority string, hb chan<- struct{}) (string, error) {
	blocks, err := m.getDetailedCoverage(ctx, packagePath, hb)
	if err != nil && len(blocks) == 0 {
		return "", err
	}

	// Tag ACCEPTED catalog gaps, then exclude them from the priority-filtered
	// JSON output (mirroring the report path's aggregateCoverageStats). A load
	// failure degrades gracefully: all gaps stay actionable.
	entries := m.loadNonFixEntries()
	applyCatalogTitles(blocks, entries)

	jsonStr, jsonErr := formatDetailedCoverageJSON(blocks, minPriority)
	if jsonErr != nil {
		return "", jsonErr
	}

	// Return JSON data even when testErr is present; caller wraps both in ToolResult
	if err != nil {
		return jsonStr, fmt.Errorf("coverage test failed (profile may be incomplete): %w", err)
	}
	return jsonStr, nil
}

// formatDetailedCoverageJSON renders the uncovered blocks as indented JSON,
// filtered by minPriority. Blocks covered by an ACCEPTED catalog entry are
// excluded from the filtered output — they are accepted gaps, not actionable
// ones, and remain visible via the catalog itself. This mirrors the report
// path (aggregateCoverageStats), which also excludes CatalogTitle != "" from
// the High/Medium buckets.
func formatDetailedCoverageJSON(blocks []uncoveredBlock, minPriority string) (string, error) {
	priorityMap := map[string]int{
		"High":   3,
		"Medium": 2,
		"Low":    1,
	}

	minP := priorityMap[minPriority]
	var filtered []uncoveredBlock
	for _, b := range blocks {
		// Cataloged (ACCEPTED) gaps are already-accepted: they must never rank
		// as actionable. They remain visible via the catalog itself.
		if b.CatalogTitle != "" {
			continue
		}
		if priorityMap[b.Priority] >= minP {
			filtered = append(filtered, b)
		}
	}

	data, err := jsonMarshalIndent(filtered, "", "  ")
	return string(data), err
}
