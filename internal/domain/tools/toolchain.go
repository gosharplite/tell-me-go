// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"errors"
)

// ToolchainRunner defines the Go toolchain capabilities consumed by the tools
// layer. It is the union of the consumer narrowing interfaces
// (analysis.AnalysisGoRunner, the developer goRunner, and the developer
// releaseGoRunner) and is the static type of the DI thread only — consumer
// fields keep their narrow views per ADR-003 Rule #2. Each method appears in
// at least one consumer narrow interface (zero dead members). Implemented by
// internal/infrastructure/toolchain.
type ToolchainRunner interface {
	GetPackageList(ctx context.Context, path string) ([]byte, error)
	GetGoDoc(ctx context.Context, symbol string) ([]byte, error)
	GetModulePath(ctx context.Context) (string, error)
	GetModuleDir(ctx context.Context) (string, error)
	RunTestsWithCoverage(ctx context.Context, path string, short bool, profilePath string) (CoverageSummary, error)
	RunLinter(ctx context.Context) (string, string, error)
	RunBenchmarks(ctx context.Context, path string, benchRegex string) (string, error)
	CheckGovulncheck(ctx context.Context) error
	RunModTidy(ctx context.Context) ([]byte, error)
	FormatCode(ctx context.Context, path string) ([]byte, error)
	RunTests(ctx context.Context, path string) ([]byte, error)
	BuildCode(ctx context.Context, outputBinary, path string) ([]byte, error)
}

// CoverageSummary is the tools-layer view of a coverage run: the verified
// 4-field consumer union of the infrastructure toolchain.CoverageReport
// (TestOutput is intentionally absent — it has zero assertion consumers; see
// ADR-060). Full report stays infrastructure-internal.
type CoverageSummary struct {
	PassedCount   int
	NoGoFiles     bool
	CoveragePct   string // e.g. "85.0%"
	SummaryOutput string
}

// ErrNoSupportedLinter is returned when neither golangci-lint nor staticcheck is found.
var ErrNoSupportedLinter = errors.New("no supported linter found (golangci-lint or staticcheck)")
