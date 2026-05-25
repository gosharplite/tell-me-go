// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import "fmt"

// anonInterfaceWarningEvaluator appends a hedge warning when a method
// name appears in anonymous-interface assertion sites (e.g.,
// x.(interface{ M() })). This is independent of the text-match warning
// — both may fire on the same orphan.
//
// This is step 7 of the original evaluateOrphan pipeline.
type anonInterfaceWarningEvaluator struct {
	analyzer *defaultDeadCodeAnalyzer
}

func (e *anonInterfaceWarningEvaluator) evaluate(ctx *orphanEvalContext) *orphanEvalContext {
	if ctx == nil || ctx.report == nil {
		return ctx
	}
	if !ctx.meta.isMethod {
		return ctx
	}
	if e.analyzer.hasAnonymousInterfaceAssertionMatch(ctx.state, ctx.meta.name) {
		ctx.report.Reason += fmt.Sprintf(
			" [WARNING: method name appears in anonymous-interface assertion site(s); verify with: grep -rn \"%s\" --include='*.go' .]",
			ctx.meta.name,
		)
	}
	return ctx
}
