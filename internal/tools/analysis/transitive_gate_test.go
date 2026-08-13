// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

// pinnedWhitelist is the exact whitelist format pinned by ADR-056 Decision 2
// and the task: three consumer entries (including the derived marker for
// internal/domain/ports) plus the events → telemetry decision note, plus the
// ADR-056 infra→infra lateral-edge backstop (issue #1350 item 6).
const pinnedWhitelist = `# Transitive Import Whitelist — ADR-056, Decision 2
Architect-curated. The architect owns whitelist maintenance...
## decision: events → telemetry
First recorded decision: ... legitimate ...
## consumer: internal/ui/tui
allowed: llm
## consumer: internal/agent/session/context
allowed: llm, tools, events, telemetry
## consumer: internal/domain/ports
allowed: <derived>

## infra-root: di
allowed: exec, factory, history, llm, logging, persistence, registry, security, skills, telemetry, toolchain
# composition root: composes leaf adapters (ADR-055/060/062 injection)

## infra-root: factory
allowed: config, llm, skills, telemetry
# composition root: constructs chatter/health-manager

## infra-sanctioned: llm → auth
# llm/factory.go constructs auth (issue #1350 item 4, ADR-055 confinement)

## infra-sanctioned: auth → persistence
# auth/default_fs.go (issue #1350 item 3)

## infra-sanctioned: history → persistence
# history/default_asset_store.go (issue #1350 item 5)
`

func TestParseTransitiveWhitelist(t *testing.T) {
	t.Parallel()

	wl, err := parseTransitiveWhitelist(pinnedWhitelist)
	require.NoError(t, err)

	require.Len(t, wl.Decisions, 1)
	assert.Equal(t, "events → telemetry", wl.Decisions[0])

	require.Len(t, wl.Consumers, 3)

	ui, ok := wl.Consumers["internal/ui/tui"]
	require.True(t, ok, "expected ui/tui entry")
	assert.Equal(t, []string{"llm"}, ui.Allowed)
	assert.False(t, ui.Derived)

	sess, ok := wl.Consumers["internal/agent/session/context"]
	require.True(t, ok, "expected sessctx entry")
	assert.Equal(t, []string{"events", "llm", "telemetry", "tools"}, sess.Allowed)
	assert.False(t, sess.Derived)

	ports, ok := wl.Consumers["internal/domain/ports"]
	require.True(t, ok, "expected ports entry")
	assert.True(t, ports.Derived)
	assert.Nil(t, ports.Allowed)

	require.Len(t, wl.Infra.Roots, 2)
	assert.Equal(t, []string{"exec", "factory", "history", "llm", "logging", "persistence", "registry", "security", "skills", "telemetry", "toolchain"}, wl.Infra.Roots["di"])
	assert.Equal(t, []string{"config", "llm", "skills", "telemetry"}, wl.Infra.Roots["factory"])
	require.Len(t, wl.Infra.Sanctioned, 3)
	assert.Equal(t, []infraEdge{
		{Source: "llm", Target: "auth"},
		{Source: "auth", Target: "persistence"},
		{Source: "history", Target: "persistence"},
	}, wl.Infra.Sanctioned)
}

func TestParseTransitiveWhitelist_Malformed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		data    string
		wantErr string
	}{
		{
			name:    "allowed outside consumer block",
			data:    "allowed: llm\n",
			wantErr: "outside a consumer block",
		},
		{
			name:    "consumer without allowed",
			data:    "## consumer: internal/ui/tui\n",
			wantErr: "no allowed: list",
		},
		{
			name:    "duplicate consumer",
			data:    "## consumer: internal/ui/tui\nallowed: llm\n## consumer: internal/ui/tui\nallowed: tools\n",
			wantErr: "duplicate consumer",
		},
		{
			name:    "derived marker on non-ports consumer",
			data:    "## consumer: internal/ui/tui\nallowed: <derived>\n",
			wantErr: "<derived> is valid only for internal/domain/ports",
		},
		{
			name:    "empty allowed list",
			data:    "## consumer: internal/ui/tui\nallowed:\n",
			wantErr: "empty allowed: list",
		},
		{
			name:    "unexpected content in consumer block",
			data:    "## consumer: internal/ui/tui\nallowed: llm\nsome prose inside a consumer block\n",
			wantErr: "unexpected content in consumer block",
		},
		{
			name:    "untracked consumer path",
			data:    "## consumer: pkg/foo\nallowed: llm\n",
			wantErr: "not a tracked internal/ or cmd/ path",
		},
		{
			name:    "empty decision note",
			data:    "## decision:\n",
			wantErr: "empty decision note",
		},
		{
			name:    "invalid family name",
			data:    "## consumer: internal/ui/tui\nallowed: LLM\n",
			wantErr: "invalid family name",
		},
		{
			name:    "infra-root without allowed",
			data:    "## infra-root: di\n",
			wantErr: "no allowed: list",
		},
		{
			name:    "duplicate infra-root",
			data:    "## infra-root: di\nallowed: llm\n## infra-root: di\nallowed: llm\n",
			wantErr: "duplicate infra-root",
		},
		{
			name:    "infra-sanctioned missing arrow",
			data:    "## infra-sanctioned: llm auth\n",
			wantErr: "must be",
		},
		{
			name:    "infra-sanctioned intra-family",
			data:    "## infra-sanctioned: llm → llm\n",
			wantErr: "intra-family",
		},
		{
			name:    "duplicate infra-sanctioned",
			data:    "## infra-sanctioned: llm → auth\n## infra-sanctioned: llm → auth\n",
			wantErr: "duplicate infra-sanctioned",
		},
		{
			name:    "invalid infra-root family",
			data:    "## infra-root: DI\nallowed: llm\n",
			wantErr: "invalid infra-root family",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseTransitiveWhitelist(tt.data)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// transitiveFixtureGraph is a hand-built import graph exercising the ports
// hub exclusion: consumer and deep reach events/llm directly and ports
// transitively; ports fans out to all 9 documented families — none of which
// are attributed to importers (the hub is a whitelisted node). events pulls
// telemetry, so Dom(ports) is the full 9-family derived constant while
// Dom(consumer) includes telemetry without the consumer importing it
// directly.
func transitiveFixtureGraph(modulePath string) map[string][]string {
	ports := modulePath + "/" + portsConsumerPath
	return map[string][]string{
		modulePath + "/internal/consumer": {
			modulePath + "/internal/domain/llm",
			modulePath + "/internal/domain/events",
			ports,
		},
		modulePath + "/internal/deep": {
			modulePath + "/internal/consumer",
		},
		modulePath + "/internal/domain/events": {
			modulePath + "/internal/domain/telemetry",
		},
		modulePath + "/internal/domain/telemetry": {},
		modulePath + "/internal/domain/llm":       {},
		ports: {
			modulePath + "/internal/domain/config",
			modulePath + "/internal/domain/events",
			modulePath + "/internal/domain/llm",
			modulePath + "/internal/domain/persistence",
			modulePath + "/internal/domain/pricing",
			modulePath + "/internal/domain/security",
			modulePath + "/internal/domain/skills",
			modulePath + "/internal/domain/telemetry",
			modulePath + "/internal/domain/tools",
		},
	}
}

func TestDomainClosure_PortsHubExcluded(t *testing.T) {
	t.Parallel()
	const modulePath = "example.com/mod"
	graph := transitiveFixtureGraph(modulePath)

	t.Run("consumer closure excludes ports hub families", func(t *testing.T) {
		t.Parallel()
		dom := domainClosure(graph, modulePath+"/internal/consumer", modulePath)
		// config/security/skills live behind ports — the hub is a whitelisted
		// node, so its closure is NOT attributed to importers.
		assert.Equal(t, []string{"events", "llm", "telemetry"}, dom)
	})

	t.Run("transitive consumer sees same closure", func(t *testing.T) {
		t.Parallel()
		dom := domainClosure(graph, modulePath+"/internal/deep", modulePath)
		assert.Equal(t, []string{"events", "llm", "telemetry"}, dom)
	})

	t.Run("ports root traverses through itself", func(t *testing.T) {
		t.Parallel()
		dom := domainClosure(graph, modulePath+"/"+portsConsumerPath, modulePath)
		// Rooted at the hub, its own edges expand to the full documented
		// 9-family derived constant.
		assert.Equal(t, derivedPortsFamilies, dom)
	})

	t.Run("leaf domain family has empty closure", func(t *testing.T) {
		t.Parallel()
		dom := domainClosure(graph, modulePath+"/internal/domain/llm", modulePath)
		assert.Empty(t, dom)
	})

	t.Run("non-domain consumer has empty closure", func(t *testing.T) {
		t.Parallel()
		dom := domainClosure(graph, modulePath+"/internal/pkg/clock", modulePath)
		assert.Empty(t, dom)
	})
}

func TestDirectDomainFamilies(t *testing.T) {
	t.Parallel()
	const modulePath = "example.com/mod"
	graph := transitiveFixtureGraph(modulePath)

	direct := directDomainFamilies(graph, modulePath+"/internal/consumer", modulePath)
	// ports excluded from direct domain imports; events + llm are the
	// consumer's own direct domain families.
	assert.Equal(t, []string{"events", "llm"}, direct)

	direct = directDomainFamilies(graph, modulePath+"/"+portsConsumerPath, modulePath)
	assert.Equal(t, derivedPortsFamilies, direct)

	direct = directDomainFamilies(graph, modulePath+"/internal/deep", modulePath)
	assert.Empty(t, direct)
}

func TestClassifyConsumer(t *testing.T) {
	t.Parallel()

	wl, err := parseTransitiveWhitelist(pinnedWhitelist)
	require.NoError(t, err)

	tests := []struct {
		name       string
		consumer   string
		dom        []string
		directDom  []string
		wantStatus consumerStatus
		wantExcess []string
		wantDetail string
	}{
		{
			name:       "whitelisted dom subset of allowed is approved constant",
			consumer:   "internal/ui/tui",
			dom:        []string{"llm"},
			directDom:  []string{"llm"},
			wantStatus: statusApprovedConstant,
			wantDetail: "whitelisted",
		},
		{
			name:       "whitelisted consumer with excess is decision required",
			consumer:   "internal/ui/tui",
			dom:        []string{"llm", "security"},
			directDom:  []string{"llm"},
			wantStatus: statusDecisionRequired,
			wantExcess: []string{"security"},
			wantDetail: "excess beyond whitelist",
		},
		{
			name:       "non-whitelisted self-justifying consumer is approved constant",
			consumer:   "internal/agent/agenttest",
			dom:        []string{"events", "llm", "telemetry"},
			directDom:  []string{"events", "llm", "telemetry"},
			wantStatus: statusApprovedConstant,
			wantDetail: "self-justifying",
		},
		{
			name:       "pure-bloat payer is decision required",
			consumer:   "internal/agent/session",
			dom:        []string{"config", "events", "llm"},
			directDom:  []string{"llm"},
			wantStatus: statusDecisionRequired,
			wantExcess: []string{"config", "events"},
			wantDetail: "pure-bloat payer",
		},
		{
			name:       "ports matching derived constant is approved",
			consumer:   portsConsumerPath,
			dom:        []string{"config", "events", "llm", "persistence", "pricing", "security", "skills", "telemetry", "tools"},
			wantStatus: statusApprovedConstant,
			wantDetail: "derived constant",
		},
		{
			name:       "ports drift is decision required",
			consumer:   portsConsumerPath,
			dom:        []string{"config", "events", "llm", "persistence", "pricing", "security", "skills", "telemetry", "tools", "billing"},
			wantStatus: statusDecisionRequired,
			wantExcess: []string{"billing"},
			wantDetail: "derived-constant drift",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := classifyConsumer(tt.consumer, tt.dom, tt.directDom, wl)
			assert.Equal(t, tt.wantStatus, got.Status)
			assert.Equal(t, tt.wantExcess, got.Excess)
			assert.Contains(t, got.Detail, tt.wantDetail)
			if tt.consumer == portsConsumerPath {
				assert.True(t, got.Derived)
			}
		})
	}

	t.Run("nil whitelist treated as no entries", func(t *testing.T) {
		t.Parallel()
		got := classifyConsumer("internal/x", []string{"config"}, []string{"config"}, nil)
		assert.Equal(t, statusApprovedConstant, got.Status)
		assert.False(t, got.Whitelisted)
	})
}

func TestInfraFamilyOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "adapter subpackage maps to family",
			in:   "internal/infrastructure/llm/anthropic",
			want: "llm",
		},
		{
			name: "composition root is its own family",
			in:   "internal/infrastructure/di",
			want: "di",
		},
		{
			name: "persistencetest maps to persistence",
			in:   "internal/infrastructure/persistence/persistencetest",
			want: "persistence",
		},
		{
			name: "domain path is not infra",
			in:   "internal/domain/llm",
			want: "",
		},
		{
			name: "bare infra prefix is empty",
			in:   "internal/infrastructure/",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, infraFamilyOf(tt.in))
		})
	}
}

func TestIsTestVariant(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pkg  *packages.Package
		want bool
	}{
		{
			name: "base production package",
			pkg:  &packages.Package{PkgPath: "X", ID: "X"},
			want: false,
		},
		{
			name: "in-package test variant",
			pkg:  &packages.Package{PkgPath: "X", ID: "X [X.test]"},
			want: true,
		},
		{
			name: "external test package",
			pkg:  &packages.Package{PkgPath: "X_test", ID: "X_test [X.test]"},
			want: true,
		},
		{
			name: "test binary",
			pkg:  &packages.Package{PkgPath: "X.test", ID: "X.test"},
			want: true,
		},
		{
			name: "nil package",
			pkg:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isTestVariant(tt.pkg))
		})
	}
}

func TestEnumerateInfraLateralEdges(t *testing.T) {
	t.Parallel()
	const modulePath = "example.com/mod"

	infra := func(rel string) string { return modulePath + "/internal/infrastructure/" + rel }

	pkgs := []*packages.Package{
		{
			PkgPath: infra("di"),
			ID:      infra("di"),
			Imports: map[string]*packages.Package{
				infra("exec"):      {},
				infra("factory"):   {},
				infra("llm"):       {},
				infra("toolchain"): {},
			},
		},
		{
			PkgPath: infra("factory"),
			ID:      infra("factory"),
			Imports: map[string]*packages.Package{
				infra("config"): {},
				infra("llm"):    {},
			},
		},
		{
			PkgPath: infra("llm"),
			ID:      infra("llm"),
			Imports: map[string]*packages.Package{
				infra("auth"):          {},
				infra("llm/anthropic"): {},
			},
		},
		{
			PkgPath: infra("llm/anthropic"),
			ID:      infra("llm/anthropic"),
			Imports: map[string]*packages.Package{
				infra("llm/llmerr"): {},
			},
		},
		{
			PkgPath: infra("auth"),
			ID:      infra("auth"),
			Imports: map[string]*packages.Package{
				infra("persistence"): {},
			},
		},
		{
			PkgPath: infra("history"),
			ID:      infra("history"),
			Imports: map[string]*packages.Package{
				infra("persistence"): {},
			},
		},
		{
			// In-package test variant of llm — excluded from enumeration.
			PkgPath: infra("llm"),
			ID:      infra("llm") + " [" + infra("llm") + ".test]",
			Imports: map[string]*packages.Package{
				infra("telemetry"): {},
			},
		},
		{
			// External test package — excluded from enumeration.
			PkgPath: infra("logging") + "_test",
			ID:      infra("logging") + "_test [" + infra("logging") + ".test]",
			Imports: map[string]*packages.Package{
				infra("persistence"): {},
			},
		},
	}

	got := enumerateInfraLateralEdges(pkgs, modulePath)
	want := []infraEdge{
		{Source: "auth", Target: "persistence"},
		{Source: "di", Target: "exec"},
		{Source: "di", Target: "factory"},
		{Source: "di", Target: "llm"},
		{Source: "di", Target: "toolchain"},
		{Source: "factory", Target: "config"},
		{Source: "factory", Target: "llm"},
		{Source: "history", Target: "persistence"},
		{Source: "llm", Target: "auth"},
	}
	assert.Equal(t, want, got)
}

func TestClassifyInfraLateralEdge(t *testing.T) {
	t.Parallel()

	wl, err := parseTransitiveWhitelist(pinnedWhitelist)
	require.NoError(t, err)

	tests := []struct {
		name       string
		src, tgt   string
		wantStatus consumerStatus
		wantDetail string
	}{
		{
			name:       "composition root in allowed set is approved",
			src:        "di",
			tgt:        "exec",
			wantStatus: statusApprovedConstant,
			wantDetail: "composition root",
		},
		{
			name:       "composition root beyond allowed set is decision required",
			src:        "factory",
			tgt:        "history",
			wantStatus: statusDecisionRequired,
			wantDetail: "beyond allowed set",
		},
		{
			name:       "sanctioned adapter edge is approved",
			src:        "llm",
			tgt:        "auth",
			wantStatus: statusApprovedConstant,
			wantDetail: "sanctioned adapter construction",
		},
		{
			name:       "unsanctioned lateral edge is decision required",
			src:        "llm",
			tgt:        "telemetry",
			wantStatus: statusDecisionRequired,
			wantDetail: "lateral adapter-to-adapter edge",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := classifyInfraLateralEdge(tt.src, tt.tgt, wl)
			assert.Equal(t, tt.wantStatus, got.Status)
			assert.Contains(t, got.Detail, tt.wantDetail)
		})
	}
}

func TestBuildInternalImportGraph_MergesTestVariants(t *testing.T) {
	t.Parallel()
	const modulePath = "example.com/mod"

	pkgs := []*packages.Package{
		{
			PkgPath: modulePath + "/internal/consumer",
			ID:      modulePath + "/internal/consumer",
			Module:  &packages.Module{Path: modulePath},
			Imports: map[string]*packages.Package{
				modulePath + "/internal/domain/llm": {},
				"example.com/external/lib":          {},
			},
		},
		{
			// In-package test variant — same PkgPath, bracketed ID — merges
			// into the base consumer (its test-only imports count).
			PkgPath: modulePath + "/internal/consumer",
			ID:      modulePath + "/internal/consumer [" + modulePath + "/internal/consumer.test]",
			Imports: map[string]*packages.Package{
				modulePath + "/internal/domain/pricing": {},
				modulePath + "/internal/domain/llm":     {},
			},
		},
		{
			// External test package — the _test suffix strips to the base.
			PkgPath: modulePath + "/internal/consumer_test",
			ID:      modulePath + "/internal/consumer_test [" + modulePath + "/internal/consumer.test]",
			Imports: map[string]*packages.Package{
				modulePath + "/internal/domain/security": {},
			},
		},
		{
			// Test binary — the .test suffix strips to the base.
			PkgPath: modulePath + "/internal/consumer.test",
			ID:      modulePath + "/internal/consumer.test",
			Imports: map[string]*packages.Package{
				modulePath + "/internal/consumer": {},
			},
		},
		{
			// Untracked package (no internal/ or cmd/) — excluded.
			PkgPath: modulePath + "/pkg/public",
			ID:      modulePath + "/pkg/public",
			Module:  &packages.Module{Path: modulePath},
			Imports: map[string]*packages.Package{},
		},
	}

	graph := buildInternalImportGraph(pkgs, modulePath)

	// One consumer row: the base and all test variants merge into a single
	// key with the union of module-internal edges (external module imports
	// dropped, self-import of the base from the test binary is a loop and is
	// dropped by the visited-set in the closure).
	require.Len(t, graph, 1)
	edges := graph[modulePath+"/internal/consumer"]
	assert.ElementsMatch(t, []string{
		modulePath + "/internal/domain/llm",
		modulePath + "/internal/domain/pricing",
		modulePath + "/internal/domain/security",
	}, edges)
	assert.NotContains(t, graph, modulePath+"/internal/consumer_test")
	assert.NotContains(t, graph, modulePath+"/internal/consumer.test")
	assert.NotContains(t, graph, modulePath+"/pkg/public")
}

func TestClassifyAllConsumers_SortedAndComplete(t *testing.T) {
	t.Parallel()
	const modulePath = "example.com/mod"
	graph := transitiveFixtureGraph(modulePath)

	wl, err := parseTransitiveWhitelist(pinnedWhitelist)
	require.NoError(t, err)

	got := classifyAllConsumers(graph, wl, modulePath)

	// Consumers: consumer, deep, events, llm, ports, telemetry — sorted by
	// full path. consumer is a pure-bloat payer (telemetry enters its closure
	// via events without being imported directly); deep is a pure-bloat payer
	// (all families arrive transitively); events/llm/telemetry are
	// self-justifying; ports is the derived constant.
	require.Len(t, got, 6)
	assert.Equal(t, "internal/consumer", got[0].Consumer)
	assert.Equal(t, "internal/deep", got[1].Consumer)
	assert.Equal(t, "internal/domain/events", got[2].Consumer)
	assert.Equal(t, "internal/domain/llm", got[3].Consumer)
	assert.Equal(t, "internal/domain/ports", got[4].Consumer)
	assert.Equal(t, "internal/domain/telemetry", got[5].Consumer)

	assert.Equal(t, statusDecisionRequired, got[0].Status)
	assert.Equal(t, []string{"telemetry"}, got[0].Excess)
	assert.Equal(t, statusDecisionRequired, got[1].Status)
	assert.Equal(t, statusApprovedConstant, got[2].Status)
	assert.Equal(t, statusApprovedConstant, got[3].Status)
	assert.Equal(t, statusApprovedConstant, got[4].Status)
	assert.True(t, got[4].Derived)
	assert.Equal(t, statusApprovedConstant, got[5].Status)
}

func TestFormatTransitiveGateReport_SeparatesSections(t *testing.T) {
	t.Parallel()
	const modulePath = "example.com/mod"
	graph := transitiveFixtureGraph(modulePath)

	wl, err := parseTransitiveWhitelist(pinnedWhitelist)
	require.NoError(t, err)
	got := formatTransitiveGateReport(classifyAllConsumers(graph, wl, modulePath), nil, wl)

	assert.Contains(t, got, "— DECISION REQUIRED (2) —")
	assert.Contains(t, got, "— APPROVED CONSTANT (4) —")
	assert.Contains(t, got, "derived constant: closure equals documented 9 families")
	assert.Contains(t, got, "Whitelist decisions (1):")
	assert.Contains(t, got, "events → telemetry")
	// The pure-bloat payer row shows consumer, whitelist-or-expected,
	// closure, and excess — no invented rationale.
	assert.Contains(t, got, "| internal/consumer | expected: direct: events, llm | events, llm, telemetry | telemetry |")

	// With a payer injected, the row shows consumer, whitelist-or-expected,
	// closure, and excess — no invented rationale.
	report := formatTransitiveGateReport([]consumerClassification{
		{
			Consumer:    "internal/ui/tui",
			Whitelisted: true,
			Allowed:     []string{"llm"},
			Dom:         []string{"llm", "security"},
			Excess:      []string{"security"},
			Status:      statusDecisionRequired,
		},
	}, []infraLateralClassification{
		{
			Source: "di",
			Target: "exec",
			Detail: "composition root: composes exec",
			Status: statusApprovedConstant,
		},
		{
			Source: "llm",
			Target: "telemetry",
			Detail: "lateral adapter-to-adapter edge",
			Status: statusDecisionRequired,
		},
	}, wl)
	assert.Contains(t, report, "| internal/ui/tui | allowed: llm | llm, security | security |")
	assert.Contains(t, report, "Infrastructure lateral edges:")
	assert.Contains(t, report, "— INFRA LATERAL —")
}

func TestConsumerPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "base package unchanged",
			in:   "example.com/mod/internal/x",
			want: "example.com/mod/internal/x",
		},
		{
			name: "external test package strips _test",
			in:   "example.com/mod/internal/x_test",
			want: "example.com/mod/internal/x",
		},
		{
			name: "test binary strips .test",
			in:   "example.com/mod/internal/x.test",
			want: "example.com/mod/internal/x",
		},
		{
			name: "test double package with test suffix is unchanged",
			in:   "example.com/mod/internal/domain/events/eventstest",
			want: "example.com/mod/internal/domain/events/eventstest",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, consumerPath(tt.in))
		})
	}
}

func TestDetectModulePath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		pkgs []*packages.Package
		want string
	}{
		{
			name: "first package with a module wins",
			pkgs: []*packages.Package{
				{PkgPath: "example.com/mod/internal/x", Module: &packages.Module{Path: "example.com/mod"}},
			},
			want: "example.com/mod",
		},
		{
			name: "no package declares a module",
			pkgs: []*packages.Package{{PkgPath: "example.com/mod/internal/x"}},
			want: "",
		},
		{
			name: "empty package list",
			pkgs: nil,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, detectModulePath(tt.pkgs))
		})
	}
}

// TestBuildInternalImportGraph_IndexerStyle exercises the graph builder
// through a mockSymbolIndex, mirroring how the arch-tagged gate test
// drives the live indexer (idx.Packages → graph).
func TestBuildInternalImportGraph_IndexerStyle(t *testing.T) {
	t.Parallel()
	const modulePath = "example.com/mod"
	mockIdx := &mockSymbolIndex{
		PackagesFunc: func(ctx context.Context, hb chan<- struct{}) ([]*packages.Package, error) {
			return []*packages.Package{
				{
					PkgPath: modulePath + "/internal/consumer",
					ID:      modulePath + "/internal/consumer",
					Module:  &packages.Module{Path: modulePath},
					Imports: map[string]*packages.Package{
						modulePath + "/internal/domain/llm": {},
					},
				},
			}, nil
		},
	}
	pkgs, err := mockIdx.Packages(context.Background(), nil)
	require.NoError(t, err)
	graph := buildInternalImportGraph(pkgs, modulePath)
	require.Contains(t, graph, modulePath+"/internal/consumer")
	assert.Equal(t, []string{modulePath + "/internal/domain/llm"}, graph[modulePath+"/internal/consumer"])
	assert.True(t, strings.HasPrefix(relPath(modulePath+"/internal/consumer", modulePath), "internal/"))
}
