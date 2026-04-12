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
	"time"

	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

const (
	LayerDomain         = "domain"
	LayerInfrastructure = "infrastructure"
	LayerApplication    = "application" // Groups agent, cli, ui, service, application
	LayerTools          = "tools"
	LayerShared         = "shared" // Cross-cutting utilities in internal/pkg
	LayerCmd            = "cmd"
	LayerTest           = "test"
	LayerUnknown        = "unknown"
)

// packageProvider defines the interface for loading package information.
type packageProvider interface {
	LoadPackages(ctx context.Context) (map[string][]string, error)
}

// architectureManager validates the project's architectural integrity.
type architectureManager struct {
	SP         domain_security.PolicyEvaluator
	Runner     AnalysisGoRunner
	idx        symbolIndex
	ModulePath string
	once       sync.Once // for ModulePath detection
	loaderOnce sync.Once // for Loader initialization
	Loader     packageProvider
}

// realpackageProvider implements packageProvider using the 'go list' command.
type realpackageProvider struct {
	m      *architectureManager
	Runner AnalysisGoRunner
}

// indexedPackageProvider implements packageProvider using the pre-scanned index.
type indexedPackageProvider struct {
	m   *architectureManager
	idx symbolIndex
}

func (p *indexedPackageProvider) LoadPackages(ctx context.Context) (map[string][]string, error) {
	pkgs, err := p.idx.Packages(ctx, nil)
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

	output, err := r.Runner.GetPackageList(ctx, "./...")
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

func (m *architectureManager) VerifyArchitecture(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	m.loaderOnce.Do(func() {
		if m.Loader == nil {
			if m.idx != nil {
				m.Loader = &indexedPackageProvider{m: m, idx: m.idx}
			} else {
				m.Loader = &realpackageProvider{m: m, Runner: m.Runner}
			}
		}
	})

	// Heartbeat while loading packages
	done := make(chan struct{})
	go m.runHeartbeat(hb, done)

	pkgs, err := m.Loader.LoadPackages(ctx)
	close(done)
	if err != nil {
		return tools.ToolResult{}, err
	}

	// Final heartbeat before processing
	m.sendHeartbeat(hb)

	violations := m.checkLayerViolations(pkgs, hb)
	violations = append(violations, m.checkCircularDependencies(pkgs, hb)...)

	if len(violations) == 0 {
		return tools.ToolResult{Text: "✅ Architectural integrity verified. No layer violations or circular dependencies detected."}, nil
	}

	return tools.ToolResult{Text: m.formatReport(violations)}, nil
}

func (m *architectureManager) runHeartbeat(hb chan<- struct{}, done <-chan struct{}) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			m.sendHeartbeat(hb)
		}
	}
}

func (m *architectureManager) sendHeartbeat(hb chan<- struct{}) {
	if hb != nil {
		select {
		case hb <- struct{}{}:
		default:
		}
	}
}

func (m *architectureManager) classify(pkgPath string) string {
	if strings.HasSuffix(pkgPath, "_test") || strings.HasSuffix(pkgPath, ".test") {
		return LayerTest
	}

	rel := strings.TrimPrefix(pkgPath, m.ModulePath)
	rel = strings.Trim(rel, "/")
	if rel == "" {
		return LayerUnknown
	}

	segments := strings.Split(rel, "/")
	if segments[0] == "cmd" {
		return LayerCmd
	}

	if segments[0] == "internal" {
		if len(segments) < 2 {
			return LayerUnknown
		}

		// Special Case: internal/service/toolchain
		if segments[1] == "service" && len(segments) > 2 && segments[2] == "toolchain" {
			return LayerInfrastructure
		}

		switch segments[1] {
		case "domain":
			return LayerDomain
		case "infrastructure":
			return LayerInfrastructure
		case "agent", "cli", "ui", "service", "application":
			return LayerApplication
		case "tools":
			return LayerTools
		case "pkg":
			return LayerShared
		default:
			return LayerUnknown
		}
	}

	return LayerUnknown
}

func (m *architectureManager) isLayer(pkgPath, layerName string) bool {
	return m.classify(pkgPath) == layerName
}

func (m *architectureManager) isCompositionRoot(pkgPath string) bool {
	return strings.Contains(pkgPath, "internal/infrastructure/di") ||
		strings.Contains(pkgPath, "internal/infrastructure/factory")
}

func (m *architectureManager) checkLayerViolations(pkgs map[string][]string, hb chan<- struct{}) []violation {
	rules := []rule{
		{
			SourceLayer: LayerDomain,
			Forbidden:   []string{LayerApplication, LayerInfrastructure, LayerTools, LayerCmd},
			Reason:      "Domain must not depend on Application, Infrastructure, Tools, or Cmd layers.",
		},
		{
			SourceLayer: LayerApplication,
			Forbidden:   []string{LayerInfrastructure, LayerTools, LayerCmd},
			Reason:      "Application layer must not depend on Infrastructure, Tools, or Cmd layers.",
		},
		{
			SourceLayer: LayerInfrastructure,
			Forbidden:   []string{LayerApplication, LayerCmd},
			Reason:      "Infrastructure layer must not depend on Application or Cmd layers.",
		},
		{
			SourceLayer: LayerTools,
			Forbidden:   []string{LayerApplication, LayerCmd},
			Reason:      "Tools layer must not depend on Application or Cmd layers.",
		},
	}

	var violations []violation

	// Sort packages for deterministic iteration
	var sortedPkgs []string
	for p := range pkgs {
		sortedPkgs = append(sortedPkgs, p)
	}
	sort.Strings(sortedPkgs)

	for i, pkg := range sortedPkgs {
		if i%10 == 0 && hb != nil {
			select {
			case hb <- struct{}{}:
			default:
			}
		}
		imports := pkgs[pkg]
		if m.isCompositionRoot(pkg) {
			// Skip source-layer rule checks for Composition Roots (DI/Factory),
			// but still check for improper cmd imports.
			violations = append(violations, m.checkGeneralCmdImport(pkg, imports, violations)...)
			continue
		}

		violations = append(violations, m.checkSinglePackageViolations(pkg, imports, rules)...)
		violations = append(violations, m.checkGeneralCmdImport(pkg, imports, violations)...)
	}

	return violations
}

func (m *architectureManager) checkSinglePackageViolations(pkg string, imports []string, rules []rule) []violation {
	var violations []violation
	shortPkg := m.shorten(pkg)

	for _, rule := range rules {
		if !m.isLayer(pkg, rule.SourceLayer) {
			continue
		}

		for _, imp := range imports {
			for _, forbidden := range rule.Forbidden {
				if m.isLayer(imp, forbidden) {
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
		if m.isLayer(imp, LayerCmd) {
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

func (m *architectureManager) checkCircularDependencies(pkgs map[string][]string, hb chan<- struct{}) []violation {
	detector := &circularDetector{
		m:       m,
		pkgs:    pkgs,
		visited: make(map[string]bool),
		onStack: make(map[string]bool),
		hb:      hb,
	}

	// Sort packages for deterministic iteration
	var sortedPkgs []string
	for p := range pkgs {
		sortedPkgs = append(sortedPkgs, p)
	}
	sort.Strings(sortedPkgs)

	for _, p := range sortedPkgs {
		if !detector.visited[p] {
			detector.findCycles(p)
		}
	}

	return detector.violations
}

type circularDetector struct {
	m          *architectureManager
	pkgs       map[string][]string
	visited    map[string]bool
	onStack    map[string]bool
	path       []string
	violations []violation
	hb         chan<- struct{}
	count      int
}

func (d *circularDetector) findCycles(u string) {
	d.count++
	if d.count%10 == 0 {
		d.m.sendHeartbeat(d.hb)
	}

	d.visited[u] = true
	d.onStack[u] = true
	d.path = append(d.path, u)

	for _, v := range d.pkgs[u] {
		if !d.visited[v] {
			d.findCycles(v)
		} else if d.onStack[v] {
			d.reportCycle(u, v)
		}
	}

	d.onStack[u] = false
	d.path = d.path[:len(d.path)-1]
}

func (d *circularDetector) reportCycle(u, v string) {
	cycleStart := -1
	for i, node := range d.path {
		if node == v {
			cycleStart = i
			break
		}
	}
	if cycleStart != -1 {
		cyclePath := append(d.path[cycleStart:], v)
		d.violations = append(d.violations, violation{
			pkg:      d.m.shorten(u),
			category: "[CIRCULAR REFERENCE]",
			target:   d.m.shorten(v),
			reason:   "Cycle: " + strings.Join(d.m.shortenList(cyclePath), " -> "),
		})
	}
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
	var layerViolations, circularRefs int
	for _, v := range violations {
		if v.category == "[LAYER VIOLATION]" {
			layerViolations++
		} else if v.category == "[CIRCULAR REFERENCE]" {
			circularRefs++
		}
	}

	var sb strings.Builder
	sb.WriteString("### Architectural Integrity Report: ❌ FAILED\n\n")

	_, _ = fmt.Fprintf(&sb, "Found **%d** layer violations and **%d** circular references.\n\n", layerViolations, circularRefs)

	sb.WriteString("| Package | Violation | Target | Reason |\n")
	sb.WriteString("| :--- | :--- | :--- | :--- |\n")

	for _, v := range violations {
		_, _ = fmt.Fprintf(&sb, "| `%s` | %s | `%s` | %s |\n", v.pkg, v.category, v.target, v.reason)
	}

	sb.WriteString("\n**Recommendation**: Follow the dependency inversion principle. High-level modules should not depend on low-level modules; both should depend on abstractions defined in the Domain layer.\n")

	return sb.String()
}
