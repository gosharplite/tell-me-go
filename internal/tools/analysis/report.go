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
func (a *defaultDeadCodeAnalyzer) formatToolResult(findings []OrphanReport) tools.ToolResult {
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
// private, producing an OrphanReport if so. The logic is decomposed into a
// pipeline of single-purpose evaluators (see orphan_evaluator.go).
func (a *defaultDeadCodeAnalyzer) evaluateOrphan(id string, meta *symMeta, state *scanState, deep bool) *OrphanReport {
	if meta.obj == nil {
		return nil
	}
	ctx := &orphanEvalContext{
		id:          id,
		meta:        meta,
		state:       state,
		deep:        deep,
		complexity:  a.calculateSymbolComplexity(meta.obj, state.pkgs),
		impact:      a.calculateImpactScore(meta.obj, state.pkgs),
		displayName: formatDisplayName(id, meta),
	}
	return a.runOrphanPipeline(ctx)
}

// runOrphanPipeline assembles evaluators conditionally based on --deep mode
// and executes them in sequence. Returns nil when any evaluator excludes the
// symbol, or the final OrphanReport when all evaluators pass.
func (a *defaultDeadCodeAnalyzer) runOrphanPipeline(ctx *orphanEvalContext) *OrphanReport {
	evaluators := a.buildEvaluatorPipeline(ctx.deep)

	for _, ev := range evaluators {
		ctx = ev.evaluate(ctx)
		if ctx == nil {
			return nil
		}
	}
	return ctx.report
}

// buildEvaluatorPipeline returns the ordered list of evaluators for the
// orphan classification pipeline. When --deep is active, the deep verification
// evaluator replaces the text-match warning evaluator.
func (a *defaultDeadCodeAnalyzer) buildEvaluatorPipeline(deep bool) []orphanEvaluator {
	evaluators := []orphanEvaluator{
		&nolintGateEvaluator{analyzer: a},
		&usageGateEvaluator{},
		&severityClassifierEvaluator{},
		&complexityReclassifierEvaluator{},
	}
	if deep {
		evaluators = append(evaluators, &deepVerificationEvaluator{analyzer: a})
	} else {
		evaluators = append(evaluators, &textMatchWarningEvaluator{analyzer: a})
	}
	evaluators = append(evaluators, &anonInterfaceWarningEvaluator{analyzer: a})
	return evaluators
}

// buildReport orchestrates the full report-building pipeline from scanState to structured findings.
func (a *defaultDeadCodeAnalyzer) buildReport(ctx context.Context, state *scanState, deep bool, hb chan<- struct{}) []OrphanReport {
	findings := a.collectOrphanFindings(ctx, state, deep, hb)
	sortOrphanReports(findings)
	return findings
}

// collectOrphanFindings iterates over all declarations and evaluates each for orphan status.
func (a *defaultDeadCodeAnalyzer) collectOrphanFindings(ctx context.Context, state *scanState, deep bool, hb chan<- struct{}) []OrphanReport {
	var findings []OrphanReport

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

		if report := a.evaluateOrphan(id, state.declarations[id], state, deep); report != nil {
			findings = append(findings, *report)
		}
	}
	return findings
}

// sortOrphanReports sorts orphan reports by impact (desc), then complexity (desc),
// then package (asc), then symbol name (asc).
func sortOrphanReports(reports []OrphanReport) {
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
