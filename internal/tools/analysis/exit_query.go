// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"fmt"
	"go/types"
	"log/slog"
	"sort"
	"strings"
)

// ADR-056, Decision 1 exit query: a seam is domain-home iff its consumers
// span non-importable layers. When the full set of consumers + implementers
// (including di) is single-layer, the seam moves to that layer's package —
// with di unwired on realignment. This query mechanically evaluates that
// criterion for the hub's seams: interface-type symbols declared in
// internal/domain/ports (module-wide evaluation would flag correctly-placed
// application interfaces as noise).
//
// Composition roots (internal/infrastructure/di, internal/infrastructure/
// factory) are EXCLUDED from the layer set: realignment always unwires di,
// so the query asks "ignoring di's wiring, is everything else one layer?".
//
// Deliberate override: this query does NOT call protectContractSymbol /
// isInterfaceSymbol protection. Zero-consumer interfaces must surface as
// orphan candidates (the interface-exemption override makes them visible).
// GatherOrphanReports / FindOrphanedSymbols behavior is untouched — this
// override is scoped to the new query only.
//
// Report-only: like the modelith-layers precedent, this query never fails
// the build. The exit query is surfaced via `cmd/deadcode -exit-query` and
// `make verify-exit-query`; the exit code stays 0.

// orphanExitLayerLabel is the Layer value recorded when an interface has no
// non-composition-root consumers and no non-composition-root implementers.
const orphanExitLayerLabel = "orphan (no non-di consumers or implementers)"

// exitStayRationales documents the ADR-056 post-ratification stays: ports
// seams whose composition-root-excluded layer set is single-layer (so the
// query surfaces them) but whose FULL criterion including di is cross-layer
// — recorded stays, not realignment candidates. The query annotates them so
// the report confirms the ADR instead of re-listing raw candidates. Source:
// ADR-056 post-ratification record (docs/adr/2026-08-contract-home-and-
// transitive-closure-gate.md). Keyed by symbol name (the query's scope is
// ports interfaces only, where names are unique).
var exitStayRationales = map[string]string{
	"Capturer":         "di signature (BootstrapperConfig) — full criterion cross-layer",
	"HistoryBrowser":   "di uiFactory binding — full criterion cross-layer",
	"HistoryEditor":    "di uiFactory binding — full criterion cross-layer",
	"UIRenderer":       "di uiFactory binding — full criterion cross-layer",
	"SessionFinalizer": "di-implemented sessionDeps — full criterion cross-layer",
	"HistoryRenderer":  "di + telemetry — recorded stay confirmed (realignment-eligibility, not an exit)",
}

// ExitCandidate is one internal/domain/ports interface whose non-composition-root
// consumers + implementers sit in a single layer (or none) — an ADR-056
// Decision 1 exit candidate for realignment adjudication.
type ExitCandidate struct {
	Symbol       string // interface type name (e.g., "Logger")
	Pkg          string // declaring package path (the ports hub)
	Layer        string // the single layer, or orphanExitLayerLabel when the set is empty
	Consumers    int    // non-composition-root usage locations
	Implementers int    // non-composition-root implementations
}

// GatherExitCandidates evaluates ADR-056 Decision 1's exit criterion for the
// internal/domain/ports seam interfaces. Report-only: it returns candidates
// and never fails the build. The pipeline state is reused from
// runAnalysisPipeline, but the query computes its own raw usage/implementation
// counts — it deliberately ignores the totalUses/externalUses protection that
// the orphan pipeline applies to interface contracts, so zero-consumer
// interfaces surface as orphan candidates.
func (a *defaultDeadCodeAnalyzer) GatherExitCandidates(ctx context.Context, path string, hb chan<- struct{}) ([]ExitCandidate, error) {
	state, err := a.runAnalysisPipeline(ctx, path, nil, false, hb)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, nil
	}
	return a.collectExitCandidates(ctx, state, hb), nil
}

// collectExitCandidates iterates the harvested declarations, keeping only
// interface-type symbols declared in internal/domain/ports, and evaluates
// each against the Decision 1 exit criterion. Results are sorted by symbol
// id (package then name) for deterministic output.
func (a *defaultDeadCodeAnalyzer) collectExitCandidates(ctx context.Context, state *scanState, hb chan<- struct{}) []ExitCandidate {
	fileToPkg := a.buildFileToPkgMap(state.pkgs)

	ids := make([]string, 0, len(state.declarations))
	for id, meta := range state.declarations {
		if isPortsInterfaceMeta(meta) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	var candidates []ExitCandidate
	for i, id := range ids {
		sendHeartbeat(i, hb)
		if candidate, ok := a.evaluateExitCandidate(ctx, state, state.declarations[id], fileToPkg, hb); ok {
			candidates = append(candidates, candidate)
		}
	}
	return candidates
}

// evaluateExitCandidate computes the layer set of one seam's
// (non-composition-root usages ∪ implementations). Single-layer sets and
// empty sets are exit candidates; multi-layer sets are not.
func (a *defaultDeadCodeAnalyzer) evaluateExitCandidate(ctx context.Context, state *scanState, meta *symMeta, fileToPkg map[string]string, hb chan<- struct{}) (ExitCandidate, bool) {
	layers := make(map[string]bool)
	consumerCount := 0
	implCount := 0

	usages, err := a.idx.GetUsages(ctx, meta.id, state.targetPath, hb)
	if err != nil {
		slog.Warn("exit query: usage lookup failed; skipping symbol", "symbol", meta.id, "error", err)
		return ExitCandidate{}, false
	}
	for _, loc := range usages {
		if a.recordExitUsageLayer(loc, fileToPkg, state.targetModule, layers) {
			consumerCount++
		}
	}

	for _, implID := range a.interfaceImplementations(ctx, meta, hb) {
		if a.recordExitImplLayer(implID, state.targetModule, layers) {
			implCount++
		}
	}

	return exitCandidateForLayers(meta, layers, consumerCount, implCount)
}

// exitCandidateForLayers turns a seam's layer set into an ExitCandidate:
// empty sets are orphan candidates, single-layer sets are candidates with
// the recorded layer and counts, and multi-layer sets are not candidates.
func exitCandidateForLayers(meta *symMeta, layers map[string]bool, consumers, implementers int) (ExitCandidate, bool) {
	switch len(layers) {
	case 0:
		return ExitCandidate{Symbol: meta.name, Pkg: meta.pkgPath, Layer: orphanExitLayerLabel}, true
	case 1:
		layer := ""
		for l := range layers {
			layer = l
		}
		return ExitCandidate{
			Symbol:       meta.name,
			Pkg:          meta.pkgPath,
			Layer:        layer,
			Consumers:    consumers,
			Implementers: implementers,
		}, true
	default:
		return ExitCandidate{}, false
	}
}

// recordExitUsageLayer classifies one usage location's package into the
// layer set. Returns false when the location is unmapped or its package is a
// composition root (both excluded from the layer set and the consumer count).
func (a *defaultDeadCodeAnalyzer) recordExitUsageLayer(loc location, fileToPkg map[string]string, modulePath string, layers map[string]bool) bool {
	pkg, ok := fileToPkg[loc.Path]
	if !ok {
		return false
	}
	base := getBasePkgPath(pkg)
	if isExitCompositionRoot(base) {
		return false
	}
	layers[classifyExitLayer(base, modulePath)] = true
	return true
}

// recordExitImplLayer classifies one implementation's package into the layer
// set. Returns false when the identity has no extractable package or its
// package is a composition root (excluded).
func (a *defaultDeadCodeAnalyzer) recordExitImplLayer(implID, modulePath string, layers map[string]bool) bool {
	implPkg := packagePathOfSymbolID(implID)
	if implPkg == "" || isExitCompositionRoot(implPkg) {
		return false
	}
	layers[classifyExitLayer(implPkg, modulePath)] = true
	return true
}

// interfaceImplementations returns the concrete method identities that
// implement meta's interface. The implementation index keys by interface
// METHOD identity (ports.Log), not the interface TYPE identity (ports.Logger),
// so the live path aggregates over the interface's method set. Fixture
// indexers (meta.obj == nil, see exit_query_test.go) serve the type key
// directly.
func (a *defaultDeadCodeAnalyzer) interfaceImplementations(ctx context.Context, meta *symMeta, hb chan<- struct{}) []string {
	if tn, ok := meta.obj.(*types.TypeName); ok {
		if iface, ok := tn.Type().Underlying().(*types.Interface); ok {
			var impls []string
			for i := 0; i < iface.NumMethods(); i++ {
				impls = append(impls, a.idx.GetImplementations(ctx, getSymbolIdentity(iface.Method(i)), hb)...)
			}
			return impls
		}
	}
	return a.idx.GetImplementations(ctx, meta.id, hb)
}

// isPortsInterfaceMeta reports whether meta is an interface-type symbol
// declared in internal/domain/ports — the hub's seams and Decision 1's
// jurisdiction. Interface methods (symType "Method") and symbols from other
// packages are out of scope.
func isPortsInterfaceMeta(meta *symMeta) bool {
	if meta == nil || !meta.isInterfaceType || meta.symType != "Type" {
		return false
	}
	return isPortsPackagePath(meta.pkgPath)
}

// isPortsPackagePath matches the internal/domain/ports package by path
// segment (a plain Contains would also match internal/domain/portsfoo).
// External test variants (…ports_test) are excluded: test-only interfaces
// are not production seams.
func isPortsPackagePath(pkgPath string) bool {
	const marker = "/internal/domain/ports"
	return strings.HasSuffix(pkgPath, marker) || strings.Contains(pkgPath, marker+"/")
}

// isExitCompositionRoot mirrors architectureManager.isCompositionRoot for
// the self-contained exit query. Composition roots are excluded because
// realignment always unwires di wiring.
func isExitCompositionRoot(pkgPath string) bool {
	return strings.Contains(pkgPath, "internal/infrastructure/di") ||
		strings.Contains(pkgPath, "internal/infrastructure/factory")
}

// classifyExitLayer is the standalone layer classifier for the exit query.
// It mirrors architectureManager.classify/classifyInternal but strips the
// _test / .test suffixes FIRST and classifies the base package path, so a
// test package counts as its base layer (an application-layer test is an
// application-layer consumer). architectureManager is deliberately NOT
// refactored — the query stays self-contained.
func classifyExitLayer(pkgPath, modulePath string) string {
	pkgPath = strings.TrimSuffix(pkgPath, "_test")
	pkgPath = strings.TrimSuffix(pkgPath, ".test")

	rel := strings.TrimPrefix(pkgPath, modulePath)
	rel = strings.Trim(rel, "/")
	if rel == "" {
		return layerUnknown
	}

	segments := strings.Split(rel, "/")
	switch segments[0] {
	case "cmd":
		return layerCmd
	case "internal":
		return classifyExitInternalLayer(segments)
	default:
		return layerUnknown
	}
}

// exitInternalLayerNames maps the first segment after internal/ to its
// layer, mirroring architectureManager.classifyInternal.
var exitInternalLayerNames = map[string]string{
	"domain":         layerDomain,
	"infrastructure": layerInfrastructure,
	"agent":          layerApplication,
	"cli":            layerApplication,
	"ui":             layerApplication,
	"service":        layerApplication,
	"application":    layerApplication,
	"tools":          layerTools,
	"pkg":            layerShared,
}

// classifyExitInternalLayer resolves the layer of an internal/ package via
// its first path segment.
func classifyExitInternalLayer(segments []string) string {
	if len(segments) < 2 {
		return layerUnknown
	}
	if layer, ok := exitInternalLayerNames[segments[1]]; ok {
		return layer
	}
	return layerUnknown
}

// packagePathOfSymbolID extracts the package path from a method identity of
// the form <pkgPath>.<Type>.<Method> (see formatMethodIdentity). Type and
// Method are Go identifiers and never contain dots, so the package path is
// everything before the last two dot-separated components. Identities with
// fewer than three components (functions, bare names) are not method
// identities and return "".
func packagePathOfSymbolID(id string) string {
	parts := strings.Split(id, ".")
	if len(parts) < 3 {
		return ""
	}
	return strings.Join(parts[:len(parts)-2], ".")
}

// FormatExitCandidates renders the exit-candidate rows for the CLI report.
// The "— EXIT CANDIDATES (ADR-056 Decision 1, report-only) —" banner is
// emitted by cmd/deadcode (see main.go); this function carries the
// composition-root exclusion note and the per-symbol rows so the report is
// self-documenting. Documented ADR-056 post-ratification stays (see
// exitStayRationales) are annotated per-row with their stay rationale
// instead of being re-listed as raw realignment candidates. Report-only: the
// output never influences the exit code.
func FormatExitCandidates(candidates []ExitCandidate) string {
	var sb strings.Builder
	sb.WriteString("ADR-056 Decision 1 exit query (report-only — never fails the build). Composition roots (internal/infrastructure/di, internal/infrastructure/factory) are excluded: realignment always unwires di wiring, so the query asks whether everything else sits in one layer.\n")

	if len(candidates) == 0 {
		sb.WriteString("  — no exit candidates: every internal/domain/ports seam's non-di consumers + implementers span multiple layers, or the seam has no non-di consumers or implementers.\n")
		return sb.String()
	}

	stays := 0
	for _, c := range candidates {
		if _, ok := exitStayRationales[c.Symbol]; ok {
			stays++
		}
	}
	newCandidates := len(candidates) - stays

	_, _ = fmt.Fprintf(&sb, "\nFound %d exit candidate(s): %d recorded stay(s) (ADR-056), %d new candidate(s) requiring adjudication:\n\n", len(candidates), stays, newCandidates)
	sb.WriteString("| symbol | layer | consumers | implementers | status |\n")
	sb.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, c := range candidates {
		status := "NEW — adjudicate"
		if rationale, ok := exitStayRationales[c.Symbol]; ok {
			status = "stay: " + rationale
		}
		_, _ = fmt.Fprintf(&sb, "| %s.%s | %s | %d | %d | %s |\n", c.Pkg, c.Symbol, c.Layer, c.Consumers, c.Implementers, status)
	}
	return sb.String()
}

// SummarizeExitCandidates returns a compact one-line governance summary when
// the candidate list contains no NEW rows (every candidate is a documented
// ADR-056 stay, or the list is empty). It returns "" when a NEW candidate is
// present — the caller must print the full FormatExitCandidates table so
// actionable rows are never hidden. Quiet mode is the CLI default.
func SummarizeExitCandidates(candidates []ExitCandidate) string {
	if len(candidates) == 0 {
		// FormatExitCandidates already prints the "no exit candidates" note.
		return ""
	}
	for _, c := range candidates {
		if _, ok := exitStayRationales[c.Symbol]; !ok {
			// A NEW candidate requiring adjudication: the caller must print
			// the full table so the actionable row is never hidden.
			return ""
		}
	}
	return fmt.Sprintf("ports governance (ADR-056 Decision 1): %d documented stay(s), 0 new candidate(s) requiring adjudication. Full table: go run ./cmd/deadcode -exit-query -exit-query-verbose\n", len(candidates))
}
