// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
	"sync"

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

// PackageProvider defines the interface for loading package information.
type PackageProvider interface {
	LoadPackages(ctx context.Context) (map[string][]string, error)
}

// ArchitectureManager validates the project's architectural integrity.
type ArchitectureManager struct {
	SP         security.SecurityProvider
	ModulePath string
	once       sync.Once
	Loader     PackageProvider
}

// RealPackageProvider implements PackageProvider using the 'go list' command.
type RealPackageProvider struct {
	m *ArchitectureManager
}

func (r *RealPackageProvider) LoadPackages(ctx context.Context) (map[string][]string, error) {
	if !r.m.SP.IsCommandAllowed("go") {
		return nil, fmt.Errorf("security policy: command 'go' is not allowed")
	}

	// Use a single call to go list -json ./...
	cmd := exec.CommandContext(ctx, "go", "list", "-json", "./...")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdout pipe for go list: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start go list: %w", err)
	}

	pkgs, err := r.decodePackageInfo(stdout)
	if err != nil {
		return nil, err
	}

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("go list command failed: %w", err)
	}

	return pkgs, nil
}

func (r *RealPackageProvider) decodePackageInfo(rd io.Reader) (map[string][]string, error) {
	pkgs := make(map[string][]string)
	dec := json.NewDecoder(rd)
	for {
		var p pkgInfo
		if err := dec.Decode(&p); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to decode go list output: %w", err)
		}

		if p.Module != nil {
			r.m.once.Do(func() {
				r.m.ModulePath = p.Module.Path
			})
		}

		// Only track packages within this module and containing "internal/" or "cmd/"
		if r.isTrackedPackage(p.ImportPath) {
			var trackedImports []string
			for _, imp := range p.Imports {
				// Only care about imports within the same module
				if strings.HasPrefix(imp, r.m.ModulePath) {
					trackedImports = append(trackedImports, imp)
				}
			}
			pkgs[p.ImportPath] = trackedImports
		}
	}
	return pkgs, nil
}

func (r *RealPackageProvider) isTrackedPackage(pkgPath string) bool {
	if !strings.HasPrefix(pkgPath, r.m.ModulePath) {
		return false
	}
	return strings.Contains(pkgPath, "internal/") || strings.Contains(pkgPath, "cmd/")
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
	if m.Loader == nil {
		m.Loader = &RealPackageProvider{m: m}
	}

	pkgs, err := m.Loader.LoadPackages(ctx)
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

func isLayer(pkgPath, layerName string) bool {
	// Pre-check for "internal/" to avoid splitting if not needed
	const internalSegment = "internal/"
	idx := strings.Index(pkgPath, internalSegment)
	if idx == -1 {
		return false
	}

	// Ensure it's exactly the segment "internal/"
	if idx > 0 && pkgPath[idx-1] != '/' {
		return false
	}

	remaining := pkgPath[idx+len(internalSegment):]
	// check if it starts with layerName and then a slash or end of string
	if !strings.HasPrefix(remaining, layerName) {
		return false
	}

	layerEnd := len(layerName)
	if len(remaining) == layerEnd || remaining[layerEnd] == '/' {
		return true
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

	// Sort packages for deterministic iteration
	var sortedPkgs []string
	for p := range pkgs {
		sortedPkgs = append(sortedPkgs, p)
	}
	sort.Strings(sortedPkgs)

	for _, pkg := range sortedPkgs {
		imports := pkgs[pkg]
		violations = append(violations, m.checkSinglePackageViolations(pkg, imports, rules)...)
		violations = append(violations, m.checkGeneralCmdImport(pkg, imports, violations)...)
	}

	return violations
}

func (m *ArchitectureManager) checkSinglePackageViolations(pkg string, imports []string, rules []Rule) []violation {
	var violations []violation
	shortPkg := m.shorten(pkg)

	for _, rule := range rules {
		if !isLayer(pkg, rule.SourceLayer) {
			continue
		}

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
	return violations
}

func (m *ArchitectureManager) checkGeneralCmdImport(pkg string, imports []string, existing []violation) []violation {
	if !strings.Contains(pkg, "internal/") {
		return nil
	}

	var found []violation
	shortPkg := m.shorten(pkg)
	for _, imp := range imports {
		if m.isCmd(imp) {
			shortTarget := m.shorten(imp)
			// Avoid duplicates if already caught by rules above or by previous iterations
			alreadyReported := false
			for _, v := range existing {
				if v.pkg == shortPkg && v.target == shortTarget {
					alreadyReported = true
					break
				}
			}

			if !alreadyReported {
				// Also check against what we already found in this call
				for _, v := range found {
					if v.pkg == shortPkg && v.target == shortTarget {
						alreadyReported = true
						break
					}
				}
			}

			if !alreadyReported {
				found = append(found, violation{
					pkg:      shortPkg,
					category: "[LAYER VIOLATION]",
					target:   shortTarget,
					reason:   "Composition Root (cmd) should not be imported by internal packages.",
				})
			}
		}
	}
	return found
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
	if m.ModulePath != "" {
		return strings.TrimPrefix(strings.TrimPrefix(pkg, m.ModulePath), "/")
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
