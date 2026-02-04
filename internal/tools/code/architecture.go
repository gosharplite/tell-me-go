// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package code

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/security"
)

type ArchitectureManager struct {
	SP security.SecurityProvider
}

type pkgInfo struct {
	ImportPath string
	Imports    []string
}

type violation struct {
	pkg      string
	category string
	target   string
	reason   string
}

func (m *ArchitectureManager) VerifyArchitecture(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	pkgs, err := m.getInternalPackages(ctx)
	if err != nil {
		return tools.ToolResult{}, err
	}

	violations := m.checkLayerViolations(pkgs)
	violations = append(violations, m.checkCircularDependencies(pkgs)...)

	if len(violations) == 0 {
		return tools.ToolResult{Text: "✅ Architectural integrity verified. No layer violations or circular dependencies detected."}, nil
	}

	return tools.ToolResult{Text: m.formatReport(violations)}, nil
}

func (m *ArchitectureManager) getInternalPackages(ctx context.Context) (map[string][]string, error) {
	// Find module name and root
	rootCmd := exec.CommandContext(ctx, "go", "list", "-m", "-f", "{{.Path}}\n{{.Dir}}")
	rootOut, err := rootCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to find module root: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(rootOut)), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("unexpected go list -m output")
	}
	modulePath := lines[0]
	moduleRoot := lines[1]

	cmd := exec.CommandContext(ctx, "go", "list", "-json", "./...")
	cmd.Dir = moduleRoot
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	pkgs := make(map[string][]string)
	dec := json.NewDecoder(stdout)
	for {
		var p pkgInfo
		if err := dec.Decode(&p); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		// Only track packages within this module and containing "internal/"
		if strings.HasPrefix(p.ImportPath, modulePath) && strings.Contains(p.ImportPath, "internal/") {
			var internalImports []string
			for _, imp := range p.Imports {
				if strings.HasPrefix(imp, modulePath) && strings.Contains(imp, "internal/") {
					internalImports = append(internalImports, imp)
				}
			}
			pkgs[p.ImportPath] = internalImports
		}
	}

	if err := cmd.Wait(); err != nil {
		return nil, err
	}

	return pkgs, nil
}

func (m *ArchitectureManager) checkLayerViolations(pkgs map[string][]string) []violation {
	var violations []violation

	for pkg, imports := range pkgs {
		shortPkg := m.shorten(pkg)

		for _, imp := range imports {
			shortImp := m.shorten(imp)

			// Rule 1: Domain must not import anything else in internal/ except other domain packages
			if strings.HasPrefix(shortPkg, "internal/domain") {
				if !strings.HasPrefix(shortImp, "internal/domain") {
					violations = append(violations, violation{
						pkg:      shortPkg,
						category: "[LAYER VIOLATION]",
						target:   shortImp,
						reason:   "Domain must not depend on other internal layers.",
					})
				}
			}

			// Rule 2: Agent must not import Tools or Fsutil
			if strings.HasPrefix(shortPkg, "internal/agent") {
				if strings.HasPrefix(shortImp, "internal/tools") || strings.HasPrefix(shortImp, "internal/fsutil") {
					violations = append(violations, violation{
						pkg:      shortPkg,
						category: "[LAYER VIOLATION]",
						target:   shortImp,
						reason:   "Application/Agent layer must not depend on Infrastructure/Tools implementations.",
					})
				}
			}

			// Rule 3: Tools/Fsutil/Security must not import Agent
			isInfra := strings.HasPrefix(shortPkg, "internal/tools") ||
				strings.HasPrefix(shortPkg, "internal/fsutil") ||
				strings.HasPrefix(shortPkg, "internal/security")

			if isInfra && strings.HasPrefix(shortImp, "internal/agent") {
				violations = append(violations, violation{
					pkg:      shortPkg,
					category: "[LAYER VIOLATION]",
					target:   shortImp,
					reason:   "Infrastructure layers must not depend on Application/Agent logic.",
				})
			}
		}
	}

	return violations
}

func (m *ArchitectureManager) checkCircularDependencies(pkgs map[string][]string) []violation {
	var violations []violation
	
	// Simplistic circular check: only check A -> B -> A for now, 
	// or more comprehensive DFS if needed.
	// For production we should do a full cycle detection.
	
	visited := make(map[string]bool)
	onStack := make(map[string]bool)
	var path []string

	var findCycles func(string)
	findCycles = func(u string) {
		visited[u] = true
		onStack[u] = true
		path = append(path, u)

		for _, v := range pkgs[u] {
			if !visited[v] {
				findCycles(v)
			} else if onStack[v] {
				// Cycle detected
				cycleStart := -1
				for i, node := range path {
					if node == v {
						cycleStart = i
						break
					}
				}
				if cycleStart != -1 {
					cyclePath := append(path[cycleStart:], v)
					violations = append(violations, violation{
						pkg:      m.shorten(u),
						category: "[CIRCULAR REFERENCE]",
						target:   m.shorten(v),
						reason:   "Cycle: " + strings.Join(m.shortenList(cyclePath), " -> "),
					})
				}
			}
		}

		onStack[u] = false
		path = path[:len(path)-1]
	}

	var sortedPkgs []string
	for p := range pkgs {
		sortedPkgs = append(sortedPkgs, p)
	}
	sort.Strings(sortedPkgs)

	for _, p := range sortedPkgs {
		if !visited[p] {
			findCycles(p)
		}
	}

	return violations
}

func (m *ArchitectureManager) shorten(pkg string) string {
	idx := strings.Index(pkg, "internal/")
	if idx == -1 {
		return pkg
	}
	return pkg[idx:]
}

func (m *ArchitectureManager) shortenList(pkgs []string) []string {
	res := make([]string, len(pkgs))
	for i, p := range pkgs {
		res[i] = m.shorten(p)
	}
	return res
}

func (m *ArchitectureManager) formatReport(violations []violation) string {
	var sb strings.Builder
	sb.WriteString("### Architectural Integrity Report: ❌ FAILED\n\n")
	sb.WriteString("| Package | Violation | Target | Reason |\n")
	sb.WriteString("| :--- | :--- | :--- | :--- |\n")

	for _, v := range violations {
		sb.WriteString(fmt.Sprintf("| `%s` | %s | `%s` | %s |\n", v.pkg, v.category, v.target, v.reason))
	}

	sb.WriteString("\n**Recommendation**: Follow the dependency inversion principle. High-level modules should not depend on low-level modules; both should depend on abstractions defined in the Domain layer.\n")

	return sb.String()
}
