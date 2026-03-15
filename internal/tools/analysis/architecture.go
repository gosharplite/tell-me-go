// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

const (
	layerDomain   = "domain"
	layerAgent    = "agent"
	layerTools    = "tools"
	layerStorage  = "infrastructure/storage"
	layerSecurity = "infrastructure/security"
)

// packageProvider defines the interface for loading package information.
type packageProvider interface {
	LoadPackages(ctx context.Context) (map[string][]string, error)
}

// architectureManager validates the project's architectural integrity.
type architectureManager struct {
	SP         domain_security.PolicyEvaluator
	Exec       tools.CommandExecutor
	idx        symbolIndex
	ModulePath string
	once       sync.Once
	Loader     packageProvider
}

// realpackageProvider implements packageProvider using the 'go list' command.
type realpackageProvider struct {
	m    *architectureManager
	Exec tools.CommandExecutor
}

// indexedPackageProvider implements packageProvider using the pre-scanned index.
type indexedPackageProvider struct {
	m   *architectureManager
	idx symbolIndex
}

func (p *indexedPackageProvider) LoadPackages(ctx context.Context) (map[string][]string, error) {
	pkgs, err := p.idx.Packages(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load architecture packages: %w", err)
	}

	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages found in index")
	}

	for _, pkg := range pkgs {
		if pkg.Module != nil {
			p.m.once.Do(func() {
				p.m.ModulePath = pkg.Module.Path
			})
			break
		}
	}

	res := make(map[string][]string)
	for _, pkg := range pkgs {
		if p.m.isTrackedPackage(pkg.PkgPath) {
			var trackedImports []string
			for impPath := range pkg.Imports {
				if strings.HasPrefix(impPath, p.m.ModulePath) {
					trackedImports = append(trackedImports, impPath)
				}
			}
			res[pkg.PkgPath] = trackedImports
		}
	}
	return res, nil
}

func (r *realpackageProvider) LoadPackages(ctx context.Context) (map[string][]string, error) {
	if !r.m.SP.IsCommandAllowed("go") {
		return nil, fmt.Errorf("security policy: command 'go' is not allowed")
	}

	output, err := r.Exec.CombinedOutput(ctx, "go", "list", "-json", "./...")
	if err != nil {
		return nil, fmt.Errorf("go list command failed: %w", err)
	}

	return r.decodePackageInfo(bytes.NewReader(output))
}

func (r *realpackageProvider) decodePackageInfo(rd io.Reader) (map[string][]string, error) {
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
		if r.m.isTrackedPackage(p.ImportPath) {
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

func (m *architectureManager) isTrackedPackage(pkgPath string) bool {
	if !strings.HasPrefix(pkgPath, m.ModulePath) {
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

type rule struct {
	SourceLayer string
	Forbidden   []string // Layer names or "cmd"
	Reason      string
}

func (m *architectureManager) VerifyArchitecture(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	if m.Loader == nil {
		if m.idx != nil {
			m.Loader = &indexedPackageProvider{m: m, idx: m.idx}
		} else {
			m.Loader = &realpackageProvider{m: m, Exec: m.Exec}
		}
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

func (m *architectureManager) isCmd(pkgPath string) bool {
	return strings.Contains(pkgPath, "/cmd/") || strings.HasSuffix(pkgPath, "/cmd")
}

func (m *architectureManager) checkLayerViolations(pkgs map[string][]string) []violation {
	rules := []rule{
		{
			SourceLayer: layerDomain,
			Forbidden:   []string{layerAgent, layerTools, layerStorage, layerSecurity},
			Reason:      "Domain must not depend on other internal layers.",
		},
		{
			SourceLayer: layerAgent,
			Forbidden:   []string{layerTools, layerStorage, "cmd"},
			Reason:      "Application/Agent layer must not depend on Infrastructure/Tools implementations or Composition Root (cmd).",
		},
		{
			SourceLayer: layerTools,
			Forbidden:   []string{layerAgent, "cmd"},
			Reason:      "Infrastructure layers must not depend on Application/Agent logic or Composition Root (cmd).",
		},
		{
			SourceLayer: layerStorage,
			Forbidden:   []string{layerAgent, "cmd"},
			Reason:      "Infrastructure layers must not depend on Application/Agent logic or Composition Root (cmd).",
		},
		{
			SourceLayer: layerSecurity,
			Forbidden:   []string{layerAgent, "cmd"},
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

func (m *architectureManager) checkSinglePackageViolations(pkg string, imports []string, rules []rule) []violation {
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

func (m *architectureManager) checkGeneralCmdImport(pkg string, imports []string, existing []violation) []violation {
	if !strings.Contains(pkg, "internal/") {
		return nil
	}

	var found []violation
	shortPkg := m.shorten(pkg)
	for _, imp := range imports {
		if m.isCmd(imp) {
			candidate := violation{
				pkg:      shortPkg,
				category: "[LAYER VIOLATION]",
				target:   m.shorten(imp),
				reason:   "Composition Root (cmd) should not be imported by internal packages.",
			}

			if !isAlreadyReported(candidate, existing) && !isAlreadyReported(candidate, found) {
				found = append(found, candidate)
			}
		}
	}
	return found
}

func isAlreadyReported(v violation, list []violation) bool {
	for _, item := range list {
		if item.pkg == v.pkg && item.target == v.target {
			return true
		}
	}
	return false
}

func (m *architectureManager) checkCircularDependencies(pkgs map[string][]string) []violation {
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

func (m *architectureManager) shorten(pkg string) string {
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

func (m *architectureManager) shortenList(pkgs []string) []string {
	res := make([]string, len(pkgs))
	for i, p := range pkgs {
		res[i] = m.shorten(p)
	}
	return res
}

func (m *architectureManager) formatReport(violations []violation) string {
	var sb strings.Builder
	sb.WriteString("### Architectural Integrity Report: ❌ FAILED\n\n")
	sb.WriteString("| Package | Violation | Target | Reason |\n")
	sb.WriteString("| :--- | :--- | :--- | :--- |\n")

	for _, v := range violations {
		_, _ = fmt.Fprintf(&sb, "| `%s` | %s | `%s` | %s |\n", v.pkg, v.category, v.target, v.reason)
	}

	sb.WriteString("\n**Recommendation**: Follow the dependency inversion principle. High-level modules should not depend on low-level modules; both should depend on abstractions defined in the Domain layer.\n")

	return sb.String()
}
