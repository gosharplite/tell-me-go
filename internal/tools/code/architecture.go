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

const (
	LayerDomain   = "domain"
	LayerAgent    = "agent"
	LayerTools    = "tools"
	LayerFsutil   = "fsutil"
	LayerSecurity = "security"
)

type ArchitectureManager struct {
	SP         security.SecurityProvider
	modulePath string
}

type pkgInfo struct {
	ImportPath string
	Imports    []string
	Module     *struct {
		Path string
		Dir  string
	}
}

type violation struct {
	pkg      string
	category string
	target   string
	reason   string
}

type Rule struct {
	SourceLayer string
	Forbidden   []string // Layer names or "cmd"
	Reason      string
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
	if !m.SP.IsCommandAllowed("go") {
		return nil, fmt.Errorf("security policy: command 'go' is not allowed")
	}

	// Use a single call to go list -json ./...
	cmd := exec.CommandContext(ctx, "go", "list", "-json", "./...")
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

		if m.modulePath == "" && p.Module != nil {
			m.modulePath = p.Module.Path
		}

		// Only track packages within this module and containing "internal/" or "cmd/"
		if strings.HasPrefix(p.ImportPath, m.modulePath) {
			isInternal := strings.Contains(p.ImportPath, "internal/")
			isCmd := strings.Contains(p.ImportPath, "cmd/")

			if isInternal || isCmd {
				var trackedImports []string
				for _, imp := range p.Imports {
					// Only care about imports within the same module
					if strings.HasPrefix(imp, m.modulePath) {
						trackedImports = append(trackedImports, imp)
					}
				}
				pkgs[p.ImportPath] = trackedImports
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		return nil, err
	}

	return pkgs, nil
}

func isLayer(pkgPath, layerName string) bool {
	parts := strings.Split(pkgPath, "/")
	for i, part := range parts {
		if part == "internal" && i+1 < len(parts) && parts[i+1] == layerName {
			return true
		}
	}
	return false
}

func (m *ArchitectureManager) isCmd(pkgPath string) bool {
	return strings.Contains(pkgPath, "/cmd/") || strings.HasSuffix(pkgPath, "/cmd")
}

func (m *ArchitectureManager) checkLayerViolations(pkgs map[string][]string) []violation {
	rules := []Rule{
		{
			SourceLayer: LayerDomain,
			Forbidden:   []string{LayerAgent, LayerTools, LayerFsutil, LayerSecurity},
			Reason:      "Domain must not depend on other internal layers.",
		},
		{
			SourceLayer: LayerAgent,
			Forbidden:   []string{LayerTools, LayerFsutil, "cmd"},
			Reason:      "Application/Agent layer must not depend on Infrastructure/Tools implementations or Composition Root (cmd).",
		},
		{
			SourceLayer: LayerTools,
			Forbidden:   []string{LayerAgent, "cmd"},
			Reason:      "Infrastructure layers must not depend on Application/Agent logic or Composition Root (cmd).",
		},
		{
			SourceLayer: LayerFsutil,
			Forbidden:   []string{LayerAgent, "cmd"},
			Reason:      "Infrastructure layers must not depend on Application/Agent logic or Composition Root (cmd).",
		},
		{
			SourceLayer: LayerSecurity,
			Forbidden:   []string{LayerAgent, "cmd"},
			Reason:      "Infrastructure layers must not depend on Application/Agent logic or Composition Root (cmd).",
		},
	}

	var violations []violation
	for pkg, imports := range pkgs {
		shortPkg := m.shorten(pkg)

		// Apply defined rules
		for _, rule := range rules {
			if isLayer(pkg, rule.SourceLayer) {
				for _, imp := range imports {
					for _, forbidden := range rule.Forbidden {
						match := false
						if forbidden == "cmd" {
							match = m.isCmd(imp)
						} else {
							match = isLayer(imp, forbidden)
						}

						if match {
							violations = append(violations, violation{
								pkg:      shortPkg,
								category: "[LAYER VIOLATION]",
								target:   m.shorten(imp),
								reason:   rule.Reason,
							})
						}
					}
				}
			}
		}

		// General rule: internal packages (except Domain) must not import cmd/
		if strings.Contains(pkg, "internal/") && !isLayer(pkg, LayerDomain) {
			for _, imp := range imports {
				if m.isCmd(imp) {
					// Avoid duplicates if already caught by rules above
					alreadyReported := false
					for _, v := range violations {
						if v.pkg == shortPkg && v.target == m.shorten(imp) {
							alreadyReported = true
							break
						}
					}
					if !alreadyReported {
						violations = append(violations, violation{
							pkg:      shortPkg,
							category: "[LAYER VIOLATION]",
							target:   m.shorten(imp),
							reason:   "Composition Root (cmd) should not be imported by internal packages.",
						})
					}
				}
			}
		}
	}

	return violations
}

func (m *ArchitectureManager) checkCircularDependencies(pkgs map[string][]string) []violation {
	var violations []violation

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
	if m.modulePath != "" && strings.HasPrefix(pkg, m.modulePath) {
		return strings.TrimPrefix(strings.TrimPrefix(pkg, m.modulePath), "/")
	}
	idx := strings.Index(pkg, "internal/")
	if idx != -1 {
		return pkg[idx:]
	}
	idx = strings.Index(pkg, "cmd/")
	if idx != -1 {
		return pkg[idx:]
	}
	return pkg
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
