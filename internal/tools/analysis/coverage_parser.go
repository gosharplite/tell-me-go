// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
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
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if hb != nil {
					select {
					case hb <- struct{}{}:
					default:
					}
				}
			}
		}
	}()
	defer close(done)

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

	// Execute coverage test (ignores error as original code did, allowing partial profiles)
	_ = m.runCoverageTest(ctx, packagePath, tempPath, hb)

	if err := validateProfile(tempPath); err != nil {
		return nil, err
	}

	cf, err := os.Open(tempPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = cf.Close()
	}()

	return parseDetailedCoverage(ctx, cf, m.Runner, os.ReadFile)
}

// getDetailedCoverageReport generates a formatted report optimized for LLM consumption.
func (m *healthManager) getDetailedCoverageReport(ctx context.Context, packagePath string, hb chan<- struct{}) (string, error) {
	blocks, err := m.getDetailedCoverage(ctx, packagePath, hb)
	if err != nil {
		return "", err
	}

	return formatDetailedCoverageReport(packagePath, blocks), nil
}

func formatDetailedCoverageReport(packagePath string, blocks []uncoveredBlock) string {
	high, medium, lowCount, catStats := aggregateCoverageStats(blocks)

	var sb strings.Builder
	renderReportSummary(&sb, packagePath, len(blocks), high, medium, lowCount, catStats)

	const maxItems = 10
	renderBlockGaps(&sb, "HIGH PRIORITY GAPS", high, maxItems)

	if len(medium) > 0 && len(high) < 5 {
		remainingSlots := maxItems - len(high)
		renderBlockGaps(&sb, "MEDIUM PRIORITY GAPS", medium, remainingSlots)
	}

	return sb.String()
}

func aggregateCoverageStats(blocks []uncoveredBlock) (high []uncoveredBlock, medium []uncoveredBlock, lowCount int, catStats map[string]int) {
	catStats = make(map[string]int)
	for _, b := range blocks {
		catStats[b.Category]++
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

func renderReportSummary(sb *strings.Builder, packagePath string, total int, high, medium []uncoveredBlock, lowCount int, catStats map[string]int) {
	_, _ = fmt.Fprintf(sb, "Detailed Coverage Report for %s\n", packagePath)
	sb.WriteString(strings.Repeat("-", len(packagePath)+29) + "\n")
	sb.WriteString("Summary:\n")
	_, _ = fmt.Fprintf(sb, "- Total Gaps: %d\n", total)
	_, _ = fmt.Fprintf(sb, "- High Priority (Architectural): %d\n", len(high))
	_, _ = fmt.Fprintf(sb, "- Medium Priority (Technical Debt): %d\n", len(medium))
	_, _ = fmt.Fprintf(sb, "- Low Priority: %d\n", lowCount)
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

// getDetailedCoverageJSON returns the uncovered blocks as a JSON string, filtered by priority.
func (m *healthManager) getDetailedCoverageJSON(ctx context.Context, packagePath string, minPriority string, hb chan<- struct{}) (string, error) {
	blocks, err := m.getDetailedCoverage(ctx, packagePath, hb)
	if err != nil {
		return "", err
	}

	return formatDetailedCoverageJSON(blocks, minPriority)
}

func formatDetailedCoverageJSON(blocks []uncoveredBlock, minPriority string) (string, error) {
	priorityMap := map[string]int{
		"High":   3,
		"Medium": 2,
		"Low":    1,
	}

	minP := priorityMap[minPriority]
	var filtered []uncoveredBlock
	for _, b := range blocks {
		if priorityMap[b.Priority] >= minP {
			filtered = append(filtered, b)
		}
	}

	data, err := json.MarshalIndent(filtered, "", "  ")
	return string(data), err
}
