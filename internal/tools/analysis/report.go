// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// formatToolResult converts orphan findings into a human-readable tool result.
func (a *defaultDeadCodeAnalyzer) formatToolResult(findings []orphanReport) tools.ToolResult {
	if len(findings) == 0 {
		return tools.ToolResult{Text: "No dead or effectively private code found."}
	}

	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "Found %d potential technical debt items:\n", len(findings))
	currentPkg := ""
	for _, f := range findings {
		if f.Pkg != currentPkg {
			_, _ = fmt.Fprintf(&sb, "\n### Package: %s\n", f.Pkg)
			currentPkg = f.Pkg
		}

		metrics := ""
		if f.Complexity > 0 || f.Impact > 0 {
			metrics = fmt.Sprintf(" (Complexity: %d, Impact: %d)", f.Complexity, f.Impact)
		}

		prefix := ""
		if f.Impact >= 3 {
			prefix = "[STRUCTURAL ANCHOR] "
		} else if f.Complexity >= 10 {
			prefix = "[HIGH COMPLEXITY] "
		}

		_, _ = fmt.Fprintf(&sb, "- %s[%s] %s (%s)%s: %s\n", prefix, f.Severity, f.Symbol, f.Type, metrics, f.Reason)
	}

	return tools.ToolResult{Text: sb.String()}
}

// formatDisplayName produces a human-readable name for a symbol, using
// `(Receiver).Method` notation for methods.
func formatDisplayName(id string, meta *symMeta) string {
	displayName := meta.name
	if meta.isMethod {
		// Safely isolate the type and method by stripping the known package path first
		suffix := strings.TrimPrefix(id, meta.pkgPath+".")
		if parts := strings.Split(suffix, "."); len(parts) == 2 {
			// Only struct methods will split into 2 parts: ["TypeName", "MethodName"]
			displayName = fmt.Sprintf("(%s).%s", parts[0], meta.name)
		}
	}
	return displayName
}

// evaluateOrphan determines whether a symbol qualifies as dead or effectively
// private, producing an orphanReport if so.
func (a *defaultDeadCodeAnalyzer) evaluateOrphan(id string, meta *symMeta, state *scanState) *orphanReport {
	// Symbols annotated with //nolint:deadcode are explicitly excluded
	// from dead-code reporting. This is a co-located, self-documenting
	// mechanism for suppressing known false positives (e.g., symbols
	// consumed through the agentinternal bridge pattern, ADR-022).
	if isNolintDeadcode(meta.obj, state) {
		return nil
	}

	total := state.totalUses[id]
	external := state.externalUses[id]

	if total > 0 && external > 0 {
		return nil
	}

	var severity, reason string
	if total == 0 {
		severity = "DEAD"
		reason = "No references found within the module (including interfaces/tests)."
	} else {
		// Implicitly: total > 0 && external == 0
		severity = "PRIVATE"
		reason = "Exported symbol is only used within its own package."
	}

	complexity := a.calculateSymbolComplexity(meta.obj, state.pkgs)
	impact := a.calculateImpactScore(meta.obj, state.pkgs)
	displayName := formatDisplayName(id, meta)

	if severity == "PRIVATE" && complexity >= 10 {
		reason = "High Priority Refactoring Candidate: can be refactored with zero external impact."
	}

	if a.hasTextMatchOutsidePackage(state, meta.name, meta.pkgPath) {
		reason += " [WARNING: Text search found potential cross-package usage. Verify this is not a false positive due to structural typing.]"
	}

	// Anonymous-interface-assertion hedge. See
	// dead_code_anon_interface.go for the full rationale. Methods only:
	// the pattern `x.(interface{ M() })` invokes a method, never a free
	// function or a type. Independent of the text-search warning above
	// — both may legitimately fire on the same orphan, in which case
	// both appear.
	if meta.isMethod && a.hasAnonymousInterfaceAssertionMatch(state, meta.name) {
		reason += fmt.Sprintf(" [WARNING: method name appears in anonymous-interface assertion site(s); verify with: grep -rn \"%s\" --include='*.go' .]", meta.name)
	}

	return &orphanReport{
		Symbol:     displayName,
		Pkg:        meta.pkgPath,
		Type:       meta.symType,
		Severity:   severity,
		Reason:     reason,
		Complexity: complexity,
		Impact:     impact,
	}
}

// buildReport orchestrates the full report-building pipeline from scanState to structured findings.
func (a *defaultDeadCodeAnalyzer) buildReport(ctx context.Context, state *scanState, hb chan<- struct{}) []orphanReport {
	findings := a.collectOrphanFindings(ctx, state, hb)
	sortOrphanReports(findings)
	return findings
}

// collectOrphanFindings iterates over all declarations and evaluates each for orphan status.
func (a *defaultDeadCodeAnalyzer) collectOrphanFindings(ctx context.Context, state *scanState, hb chan<- struct{}) []orphanReport {
	var findings []orphanReport

	// Sort IDs for deterministic iteration
	ids := make([]string, 0, len(state.declarations))
	for id := range state.declarations {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for i, id := range ids {
		if i%20 == 0 && hb != nil {
			select {
			case hb <- struct{}{}:
			default:
			}
		}

		if report := a.evaluateOrphan(id, state.declarations[id], state); report != nil {
			findings = append(findings, *report)
		}
	}
	return findings
}

// sortOrphanReports sorts orphan reports by impact (desc), then complexity (desc),
// then package (asc), then symbol name (asc).
func sortOrphanReports(reports []orphanReport) {
	sort.Slice(reports, func(i, j int) bool {
		if reports[i].Impact != reports[j].Impact {
			return reports[i].Impact > reports[j].Impact
		}
		if reports[i].Complexity != reports[j].Complexity {
			return reports[i].Complexity > reports[j].Complexity
		}
		if reports[i].Pkg != reports[j].Pkg {
			return reports[i].Pkg < reports[j].Pkg
		}
		return reports[i].Symbol < reports[j].Symbol
	})
}
