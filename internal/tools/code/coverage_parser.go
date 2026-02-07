// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package code

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// UncoveredBlock represents a block of code with zero coverage.
type UncoveredBlock struct {
	File     string `json:"file"`
	Start    int    `json:"start"`
	End      int    `json:"end"`
	Stmts    int    `json:"stmts"`
	Code     string `json:"code,omitempty"`
	Category string `json:"category"`
	Priority string `json:"priority"`
}

// Classify categorizes the block and assigns a priority based on heuristics.
func (b *UncoveredBlock) Classify() {
	// Categorize by content
	isErrorHandling := false
	lowerCode := strings.ToLower(b.Code)
	if strings.Contains(lowerCode, "if err != nil") ||
		strings.Contains(lowerCode, "return") && strings.Contains(lowerCode, "err") ||
		strings.Contains(lowerCode, "fmt.errorf") ||
		strings.Contains(lowerCode, "errors.new") {
		isErrorHandling = true
	}

	// Categorize by path
	isBusinessLogic := false
	isAdapter := false

	pathParts := []string{"internal/service", "internal/services", "internal/domain", "internal/usecase", "internal/agent"}
	for _, p := range pathParts {
		if strings.Contains(b.File, p) {
			isBusinessLogic = true
			break
		}
	}

	adapterParts := []string{"internal/repository", "internal/gateway", "internal/transport", "internal/api", "internal/auth"}
	for _, p := range adapterParts {
		if strings.Contains(b.File, p) {
			isAdapter = true
			break
		}
	}

	if isErrorHandling {
		b.Category = "ERROR_HANDLING"
	} else if isBusinessLogic {
		b.Category = "BUSINESS_LOGIC"
	} else if isAdapter {
		b.Category = "ADAPTER"
	} else {
		b.Category = "OTHER"
	}

	// Assign Priority
	if isErrorHandling && isBusinessLogic {
		b.Priority = "High"
	} else if isErrorHandling && isAdapter {
		b.Priority = "Medium"
	} else if isErrorHandling {
		b.Priority = "Medium" // Error handling elsewhere is still medium
	} else if isBusinessLogic {
		b.Priority = "Medium"
	} else {
		b.Priority = "Low"
	}
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

	// 1-based to 0-based conversion
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

// commandRunner is a function type for executing commands.
type commandRunner func(name string, arg ...string) ([]byte, error)

// ShellRunner is the default implementation of commandRunner using exec.Command.
func ShellRunner(name string, arg ...string) ([]byte, error) {
	return exec.Command(name, arg...).CombinedOutput()
}

func getModuleName(run commandRunner) string {
	out, err := run("go", "list", "-m")
	if err != nil {
		return ""
	}
	mod := strings.TrimSpace(string(out))
	if mod != "" && !strings.HasSuffix(mod, "/") {
		mod += "/"
	}
	return mod
}

// ParseCoverageProfile parses a go coverage profile and returns blocks with zero coverage.
func ParseCoverageProfile(r io.Reader, run commandRunner) ([]UncoveredBlock, error) {
	var blocks []UncoveredBlock
	scanner := bufio.NewScanner(r)
	modulePrefix := getModuleName(run)

	// Skip the first line (mode: ...)
	if !scanner.Scan() {
		return nil, scanner.Err()
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		// Format: file.go:startline.startcol,endline.endcol numstmt count
		parts := strings.Fields(line)
		if len(parts) != 3 {
			continue
		}

		count, err := strconv.Atoi(parts[2])
		if err != nil {
			continue
		}

		if count > 0 {
			continue
		}

		stmts, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}

		pathAndRange := parts[0]
		colonIdx := strings.LastIndex(pathAndRange, ":")
		if colonIdx == -1 {
			continue
		}

		file := pathAndRange[:colonIdx]
		// Strip module prefix if it exists to make path relative to project root
		if modulePrefix != "" && strings.HasPrefix(file, modulePrefix) {
			file = file[len(modulePrefix):]
		}
		rangePart := pathAndRange[colonIdx+1:]

		// startline.startcol,endline.endcol
		rangeParts := strings.Split(rangePart, ",")
		if len(rangeParts) != 2 {
			continue
		}

		startPart := strings.Split(rangeParts[0], ".")
		if len(startPart) < 1 {
			continue
		}
		endPart := strings.Split(rangeParts[1], ".")
		if len(endPart) < 1 {
			continue
		}

		startLine, _ := strconv.Atoi(startPart[0])
		endLine, _ := strconv.Atoi(endPart[0])

		blocks = append(blocks, UncoveredBlock{
			File:  file,
			Start: startLine,
			End:   endLine,
			Stmts: stmts,
		})
	}

	return blocks, scanner.Err()
}

// GetDetailedCoverage executes the coverage test and parses the profile.
func GetDetailedCoverage(packagePath string, run commandRunner) ([]UncoveredBlock, error) {
	f, err := os.CreateTemp("", "coverage-*.out")
	if err != nil {
		return nil, err
	}
	tempPath := f.Name()
	defer os.Remove(tempPath)
	f.Close()

	_, _ = run("go", "test", "-coverprofile="+tempPath, packagePath)
	// We ignore the error from run() because even if tests fail,
	// the coverage profile might still be generated for the parts that did run.

	info, err := os.Stat(tempPath)
	if err != nil {
		return nil, fmt.Errorf("coverage profile was not generated: %w", err)
	}
	if info.Size() == 0 {
		return nil, fmt.Errorf("coverage profile is empty; check if package path is valid and contains testable Go files")
	}

	cf, err := os.Open(tempPath)
	if err != nil {
		return nil, err
	}
	defer cf.Close()

	blocks, err := ParseCoverageProfile(cf, run)
	if err != nil {
		return nil, err
	}

	fileCache := make(map[string][]string)
	for i := range blocks {
		lines, ok := fileCache[blocks[i].File]
		if !ok {
			content, err := os.ReadFile(blocks[i].File)
			if err != nil {
				blocks[i].Code = "[Error reading file: " + err.Error() + "]"
				blocks[i].Classify()
				continue
			}
			lines = strings.Split(string(content), "\n")
			fileCache[blocks[i].File] = lines
		}

		blocks[i].Code = extractFromLines(lines, blocks[i].Start, blocks[i].End)
		blocks[i].Classify()
	}

	return blocks, nil
}

// GetDetailedCoverageReport generates a formatted report optimized for LLM consumption.
func GetDetailedCoverageReport(packagePath string, run commandRunner) (string, error) {
	blocks, err := GetDetailedCoverage(packagePath, run)
	if err != nil {
		return "", err
	}

	high := make([]UncoveredBlock, 0)
	medium := make([]UncoveredBlock, 0)
	lowCount := 0

	catStats := make(map[string]int)

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

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Detailed Coverage Report for %s\n", packagePath))
	sb.WriteString(strings.Repeat("-", len(packagePath)+29) + "\n")
	sb.WriteString("Summary:\n")
	sb.WriteString(fmt.Sprintf("- Total Gaps: %d\n", len(blocks)))
	sb.WriteString(fmt.Sprintf("- High Priority (Architectural): %d\n", len(high)))
	sb.WriteString(fmt.Sprintf("- Medium Priority (Technical Debt): %d\n", len(medium)))
	sb.WriteString(fmt.Sprintf("- Low Priority: %d\n", lowCount))
	sb.WriteString("\nBreakdown by Category:\n")
	for cat, count := range catStats {
		sb.WriteString(fmt.Sprintf("- %s: %d\n", cat, count))
	}

	const maxItems = 10

	if len(high) > 0 {
		sb.WriteString("\n[HIGH PRIORITY GAPS]\n")
		for i, b := range high {
			if i >= maxItems {
				sb.WriteString(fmt.Sprintf("... and %d more High priority gaps.\n", len(high)-maxItems))
				break
			}
			sb.WriteString(fmt.Sprintf("%d. File: %s (Lines %d-%d)\n", i+1, b.File, b.Start, b.End))
			sb.WriteString(fmt.Sprintf("   Category: %s\n", b.Category))
			sb.WriteString(fmt.Sprintf("   Code:\n%s\n\n", b.Code))
		}
	}

	// Show Medium if High are few
	if len(medium) > 0 && len(high) < 5 {
		sb.WriteString("\n[MEDIUM PRIORITY GAPS]\n")
		remainingSlots := maxItems - len(high)
		if remainingSlots <= 0 {
			remainingSlots = 5 // Minimum of 5 if we show them at all
		}
		for i, b := range medium {
			if i >= remainingSlots {
				sb.WriteString(fmt.Sprintf("... and %d more Medium priority gaps.\n", len(medium)-remainingSlots))
				break
			}
			sb.WriteString(fmt.Sprintf("%d. File: %s (Lines %d-%d)\n", i+1, b.File, b.Start, b.End))
			sb.WriteString(fmt.Sprintf("   Category: %s\n", b.Category))
			sb.WriteString(fmt.Sprintf("   Code:\n%s\n\n", b.Code))
		}
	}

	return sb.String(), nil
}

// GetDetailedCoverageJSON returns the uncovered blocks as a JSON string, filtered by priority.
func GetDetailedCoverageJSON(packagePath string, minPriority string, run commandRunner) (string, error) {
	blocks, err := GetDetailedCoverage(packagePath, run)
	if err != nil {
		return "", err
	}

	priorityMap := map[string]int{
		"High":   3,
		"Medium": 2,
		"Low":    1,
		"":       0,
	}

	minP := priorityMap[minPriority]
	var filtered []UncoveredBlock
	for _, b := range blocks {
		if priorityMap[b.Priority] >= minP {
			filtered = append(filtered, b)
		}
	}

	data, err := json.MarshalIndent(filtered, "", "  ")
	if err != nil {
		return "", err
	}

	return string(data), nil
}
