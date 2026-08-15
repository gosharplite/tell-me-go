// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"errors"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// exitTestModule is the synthetic module path used by the exit-query
// fixtures. No live repo is needed: every test runs against an in-memory
// fixtureIndexer snapshot.
const exitTestModule = "example.com/test"

// exitFixturePackages maps module-relative file paths to the package paths
// of the fixture workspace. It covers the domain hub, an application-layer
// package, an infrastructure-layer package, and both composition roots
// (di + factory) whose wiring the query must exclude.
var exitFixturePackages = map[string]string{
	"internal/domain/ports/ports.go":            exitTestModule + "/internal/domain/ports",
	"internal/application/usecase/svc.go":       exitTestModule + "/internal/application/usecase",
	"internal/infrastructure/persistence/db.go": exitTestModule + "/internal/infrastructure/persistence",
	"internal/infrastructure/di/container.go":   exitTestModule + "/internal/infrastructure/di",
	"internal/infrastructure/factory/wiring.go": exitTestModule + "/internal/infrastructure/factory",
}

// newExitWorkspace creates a temp workspace root and a base snapshot with
// the standard fixture package map. Tests customize Declarations,
// UsagesByName, and ImplsCache before running the query.
func newExitWorkspace(t *testing.T) (string, *indexSnapshot) {
	t.Helper()
	root := t.TempDir()
	fileToPkg := make(map[string]string, len(exitFixturePackages))
	for k, v := range exitFixturePackages {
		fileToPkg[k] = v
	}
	return root, &indexSnapshot{
		ModulePath:   exitTestModule,
		FileToPkg:    fileToPkg,
		UsagesByName: make(map[string][]location),
		ImplsCache:   make(map[string][]string),
	}
}

// runExitQueryAt runs GatherExitCandidates against a fixture snapshot rooted
// at root and returns the candidates.
func runExitQueryAt(t *testing.T, root string, snap *indexSnapshot) []ExitCandidate {
	t.Helper()
	analyzer := newDeadCodeAnalyzer(&deadCodeSecurityProvider{tempDir: root}, newFixtureIndexer(snap, root))
	candidates, err := analyzer.GatherExitCandidates(context.Background(), root, nil)
	require.NoError(t, err)
	return candidates
}

// exitLoc builds a usage location inside the fixture workspace.
func exitLoc(root, rel string) location {
	return location{Path: filepath.Join(root, rel)}
}

// exitPortsInterface builds the symMeta of an interface-type symbol declared
// in internal/domain/ports.
func exitPortsInterface(name string) *symMeta {
	pkg := exitTestModule + "/internal/domain/ports"
	return &symMeta{
		id:              pkg + "." + name,
		pkgPath:         pkg,
		name:            name,
		symType:         "Type",
		isInterfaceType: true,
	}
}

func TestExitQuery_SingleLayerCandidate(t *testing.T) {
	t.Parallel()
	root, snap := newExitWorkspace(t)
	svc := exitPortsInterface("Service")

	snap.Declarations = []*symMeta{svc}
	// Only consumers: an application-layer use case. No implementations.
	snap.UsagesByName[svc.id] = []location{exitLoc(root, "internal/application/usecase/svc.go")}

	candidates := runExitQueryAt(t, root, snap)
	require.Len(t, candidates, 1)

	c := candidates[0]
	assert.Equal(t, "Service", c.Symbol)
	assert.Equal(t, exitTestModule+"/internal/domain/ports", c.Pkg)
	assert.Equal(t, layerApplication, c.Layer)
	assert.Equal(t, 1, c.Consumers)
	assert.Equal(t, 0, c.Implementers)
}

func TestExitQuery_SingleLayerCandidate_UsagesAndImplementers(t *testing.T) {
	t.Parallel()
	root, snap := newExitWorkspace(t)
	svc := exitPortsInterface("Service")

	snap.Declarations = []*symMeta{svc}
	snap.UsagesByName[svc.id] = []location{
		exitLoc(root, "internal/application/usecase/svc.go"),
	}
	// Implementers keyed under the interface TYPE identity (fixture path:
	// see interfaceImplementations fallback in exit_query.go).
	snap.ImplsCache[svc.id] = []string{
		exitTestModule + "/internal/application/usecase.Worker.Do",
	}

	candidates := runExitQueryAt(t, root, snap)
	require.Len(t, candidates, 1)

	c := candidates[0]
	assert.Equal(t, layerApplication, c.Layer)
	assert.Equal(t, 1, c.Consumers)
	assert.Equal(t, 1, c.Implementers)
}

func TestExitQuery_MultiLayer_NotCandidate(t *testing.T) {
	t.Parallel()
	root, snap := newExitWorkspace(t)
	store := exitPortsInterface("Store")

	snap.Declarations = []*symMeta{store}
	// Consumers span application + infrastructure → multi-layer → NOT an
	// exit candidate.
	snap.UsagesByName[store.id] = []location{
		exitLoc(root, "internal/application/usecase/svc.go"),
		exitLoc(root, "internal/infrastructure/persistence/db.go"),
	}
	snap.ImplsCache[store.id] = []string{
		exitTestModule + "/internal/infrastructure/persistence.DB.Save",
	}

	candidates := runExitQueryAt(t, root, snap)
	assert.Empty(t, candidates)
}

func TestExitQuery_DIOnlyConsumer_CandidateFromRemainingSet(t *testing.T) {
	t.Parallel()
	root, snap := newExitWorkspace(t)
	cfg := exitPortsInterface("Config")

	snap.Declarations = []*symMeta{cfg}
	// The ONLY consumer is the di composition root — excluded from the
	// layer set and the consumer count. The remaining set comes from the
	// infrastructure implementers → single layer → exit candidate.
	snap.UsagesByName[cfg.id] = []location{
		exitLoc(root, "internal/infrastructure/di/container.go"),
	}
	snap.ImplsCache[cfg.id] = []string{
		exitTestModule + "/internal/infrastructure/persistence.DB.Load",
	}

	candidates := runExitQueryAt(t, root, snap)
	require.Len(t, candidates, 1)

	c := candidates[0]
	assert.Equal(t, "Config", c.Symbol)
	assert.Equal(t, layerInfrastructure, c.Layer)
	assert.Equal(t, 0, c.Consumers) // di usage excluded
	assert.Equal(t, 1, c.Implementers)
}

func TestExitQuery_ZeroConsumerInterface_OrphanCandidate(t *testing.T) {
	t.Parallel()
	root, snap := newExitWorkspace(t)
	health := exitPortsInterface("Health")

	// No usages, no implementers. The deliberate override (no
	// protectContractSymbol / isInterfaceSymbol protection in this query)
	// makes the zero-consumer interface surface as an orphan candidate.
	snap.Declarations = []*symMeta{health}

	candidates := runExitQueryAt(t, root, snap)
	require.Len(t, candidates, 1)

	c := candidates[0]
	assert.Equal(t, "Health", c.Symbol)
	assert.Equal(t, orphanExitLayerLabel, c.Layer)
	assert.Equal(t, 0, c.Consumers)
	assert.Equal(t, 0, c.Implementers)
}

func TestExitQuery_CompositionRootExclusion(t *testing.T) {
	t.Parallel()
	root, snap := newExitWorkspace(t)
	sp := exitPortsInterface("SessionProvider")

	snap.Declarations = []*symMeta{sp}
	// Consumers: one di usage (composition root) + one application usage.
	// WITHOUT exclusion the layer set would be
	// {infrastructure(di), application} → multi-layer → skipped; WITH
	// exclusion it is {application} → exit candidate.
	snap.UsagesByName[sp.id] = []location{
		exitLoc(root, "internal/infrastructure/di/container.go"),
		exitLoc(root, "internal/application/usecase/svc.go"),
	}
	snap.ImplsCache[sp.id] = []string{
		exitTestModule + "/internal/infrastructure/factory.SessionFactory.Create",
	}

	candidates := runExitQueryAt(t, root, snap)
	require.Len(t, candidates, 1)

	c := candidates[0]
	assert.Equal(t, "SessionProvider", c.Symbol)
	assert.Equal(t, layerApplication, c.Layer)
	assert.Equal(t, 1, c.Consumers) // only the application usage counted
	assert.Equal(t, 0, c.Implementers)
}

// TestExitQuery_InterfaceMethodAggregation exercises the live aggregation
// path: the implementation index keys by interface METHOD identity, so the
// query aggregates GetImplementations over the interface's method set when
// the declaration carries a types.Object. The fixture simulates that by
// attaching a synthetic *types.TypeName whose Type() is an interface with
// two methods and keying ImplsCache under the method identities.
func TestExitQuery_InterfaceMethodAggregation(t *testing.T) {
	t.Parallel()
	root, snap := newExitWorkspace(t)
	portsPkg := types.NewPackage(exitTestModule+"/internal/domain/ports", "ports")

	saveSig := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(types.NewVar(token.NoPos, portsPkg, "item", types.Typ[types.String])), nil, false)
	saveMethod := types.NewFunc(token.NoPos, portsPkg, "Save", saveSig)
	loadSig := types.NewSignatureType(nil, nil, nil, nil,
		types.NewTuple(types.NewVar(token.NoPos, portsPkg, "", types.Typ[types.String])), false)
	loadMethod := types.NewFunc(token.NoPos, portsPkg, "Load", loadSig)
	iface := types.NewInterfaceType([]*types.Func{saveMethod, loadMethod}, nil)
	typeName := types.NewTypeName(token.NoPos, portsPkg, "Store", iface)

	store := exitPortsInterface("Store")
	store.obj = typeName // live path: implementations come from methods

	snap.Declarations = []*symMeta{store}
	// Method identities as the index keys them: getSymbolIdentity of an
	// interface method yields <pkg>.<Interface>.<Method> (verified against
	// the real indexer — the interface's method set carries the receiver).
	for i := 0; i < iface.NumMethods(); i++ {
		methodID := getSymbolIdentity(iface.Method(i))
		switch iface.Method(i).Name() {
		case "Save":
			snap.ImplsCache[methodID] = []string{
				exitTestModule + "/internal/infrastructure/persistence.DB.Save",
			}
		case "Load":
			snap.ImplsCache[methodID] = []string{
				exitTestModule + "/internal/infrastructure/persistence.DB.Load",
			}
		}
	}

	candidates := runExitQueryAt(t, root, snap)
	require.Len(t, candidates, 1)

	c := candidates[0]
	assert.Equal(t, "Store", c.Symbol)
	assert.Equal(t, layerInfrastructure, c.Layer)
	assert.Equal(t, 0, c.Consumers)
	assert.Equal(t, 2, c.Implementers) // Save + Load, both infrastructure
}

func TestExitQuery_NonPortsInterfaceOutOfScope(t *testing.T) {
	t.Parallel()
	root, snap := newExitWorkspace(t)
	portsPkg := exitTestModule + "/internal/domain/ports"

	// An application-layer interface is NOT in scope: module-wide
	// evaluation would flag correctly-placed application interfaces as
	// noise, so the scope is restricted to the hub's seams.
	appIface := &symMeta{
		id:              exitTestModule + "/internal/application/usecase.Handler",
		pkgPath:         exitTestModule + "/internal/application/usecase",
		name:            "Handler",
		symType:         "Type",
		isInterfaceType: true,
	}
	portsIface := exitPortsInterface("Logger")

	snap.Declarations = []*symMeta{appIface, portsIface}
	// Both are consumed from application — only the ports interface is a
	// candidate; the application interface is out of scope.
	snap.UsagesByName[appIface.id] = []location{
		exitLoc(root, "internal/application/usecase/svc.go"),
	}
	snap.UsagesByName[portsIface.id] = []location{
		exitLoc(root, "internal/application/usecase/svc.go"),
	}

	candidates := runExitQueryAt(t, root, snap)
	require.Len(t, candidates, 1)
	assert.Equal(t, "Logger", candidates[0].Symbol)
	assert.Equal(t, portsPkg, candidates[0].Pkg)
	assert.Equal(t, layerApplication, candidates[0].Layer)
}

func TestExitQuery_ErrorPath(t *testing.T) {
	t.Parallel()
	analyzer := newDeadCodeAnalyzer(&denyingSecurityProvider{}, &mockSymbolIndex{})
	_, err := analyzer.GatherExitCandidates(context.Background(), "/some/path", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path not authorized")
}

func TestExitQuery_NoPackages(t *testing.T) {
	t.Parallel()
	root, snap := newExitWorkspace(t)
	snap.FileToPkg = map[string]string{}
	candidates := runExitQueryAt(t, root, snap)
	assert.Empty(t, candidates)
}

func TestFormatExitCandidates(t *testing.T) {
	t.Parallel()

	t.Run("rows rendered", func(t *testing.T) {
		t.Parallel()
		candidates := []ExitCandidate{
			{Symbol: "Logger", Pkg: exitTestModule + "/internal/domain/ports", Layer: layerApplication, Consumers: 2, Implementers: 0},
			{Symbol: "Config", Pkg: exitTestModule + "/internal/domain/ports", Layer: orphanExitLayerLabel, Consumers: 0, Implementers: 0},
		}
		out := FormatExitCandidates(candidates)

		assert.Contains(t, out, "ADR-056 Decision 1")
		assert.Contains(t, out, "report-only")
		assert.Contains(t, out, "internal/infrastructure/di")
		assert.Contains(t, out, "internal/infrastructure/factory")
		assert.Contains(t, out, "| "+exitTestModule+"/internal/domain/ports.Logger | application | 2 | 0 | NEW — adjudicate |")
		assert.Contains(t, out, "| "+exitTestModule+"/internal/domain/ports.Config | "+orphanExitLayerLabel+" | 0 | 0 | NEW — adjudicate |")
	})

	t.Run("documented stay annotated", func(t *testing.T) {
		t.Parallel()
		candidates := []ExitCandidate{
			{Symbol: "Capturer", Pkg: exitTestModule + "/internal/domain/ports", Layer: layerApplication, Consumers: 28, Implementers: 416},
		}
		out := FormatExitCandidates(candidates)

		assert.Contains(t, out, "1 recorded stay(s) (ADR-056), 0 new candidate(s)")
		assert.Contains(t, out, "stay: di signature (BootstrapperConfig)")
	})

	t.Run("no candidates", func(t *testing.T) {
		t.Parallel()
		out := FormatExitCandidates(nil)
		assert.Contains(t, out, "no exit candidates")
		assert.Contains(t, out, "ADR-056 Decision 1")
	})
}

func TestClassifyExitLayer(t *testing.T) {
	t.Parallel()
	const module = "example.com/test"

	tests := []struct {
		pkg  string
		want string
	}{
		{module + "/internal/domain/events", layerDomain},
		{module + "/internal/domain/ports", layerDomain},
		{module + "/internal/infrastructure/logging", layerInfrastructure},
		{module + "/internal/agent", layerApplication},
		{module + "/internal/cli", layerApplication},
		{module + "/internal/ui", layerApplication},
		{module + "/internal/service", layerApplication},
		{module + "/internal/application/suggestions", layerApplication},
		{module + "/internal/app", layerApplication},
		{module + "/internal/tools/analysis", layerTools},
		{module + "/internal/pkg/clock", layerShared},
		{module + "/cmd/deadcode", layerCmd},
		// Test suffixes are stripped and the base path classified.
		{module + "/internal/infrastructure/persistence_test", layerInfrastructure},
		{module + "/internal/infrastructure/persistence.test", layerInfrastructure},
		{module + "/internal/application/usecase", layerApplication},
		{module + "/internal/notalayer", layerUnknown},
		{module + "/something", layerUnknown},
		{"", layerUnknown},
	}
	for _, tt := range tests {
		got := classifyExitLayer(tt.pkg, module)
		assert.Equal(t, tt.want, got, "classifyExitLayer(%q)", tt.pkg)
	}
}

func TestPackagePathOfSymbolID(t *testing.T) {
	t.Parallel()
	const module = "example.com/test"

	assert.Equal(t,
		module+"/internal/infrastructure/persistence",
		packagePathOfSymbolID(module+"/internal/infrastructure/persistence.DB.Save"))
	// Non-method identities (fewer than three dot-separated components) have
	// no extractable package path.
	assert.Equal(t, "", packagePathOfSymbolID("pkg.Func"))
	assert.Equal(t, "", packagePathOfSymbolID("no-dots"))
}

func TestIsExitCompositionRoot(t *testing.T) {
	t.Parallel()
	const module = "example.com/test"

	assert.True(t, isExitCompositionRoot(module+"/internal/infrastructure/di"))
	assert.True(t, isExitCompositionRoot(module+"/internal/infrastructure/di/container"))
	assert.True(t, isExitCompositionRoot(module+"/internal/infrastructure/factory"))
	assert.False(t, isExitCompositionRoot(module+"/internal/infrastructure/persistence"))
	assert.False(t, isExitCompositionRoot(module+"/internal/domain/ports"))
}

func TestIsPortsPackagePath(t *testing.T) {
	t.Parallel()
	const module = "example.com/test"

	assert.True(t, isPortsPackagePath(module+"/internal/domain/ports"))
	// External test variants are NOT production seams.
	assert.False(t, isPortsPackagePath(module+"/internal/domain/ports_test"))
	// Prefix collision must not match.
	assert.False(t, isPortsPackagePath(module+"/internal/domain/portsfoo"))
	assert.False(t, isPortsPackagePath(module+"/internal/domain/config"))
}

func TestIsPortsInterfaceMeta(t *testing.T) {
	t.Parallel()

	t.Run("interface type in ports", func(t *testing.T) {
		t.Parallel()
		assert.True(t, isPortsInterfaceMeta(exitPortsInterface("Logger")))
	})

	t.Run("interface method excluded", func(t *testing.T) {
		t.Parallel()
		meta := &symMeta{
			id:                exitTestModule + "/internal/domain/ports.Log",
			pkgPath:           exitTestModule + "/internal/domain/ports",
			name:              "Log",
			symType:           "Method",
			isMethod:          true,
			isInterfaceMethod: true,
		}
		assert.False(t, isPortsInterfaceMeta(meta))
	})

	t.Run("non-interface type excluded", func(t *testing.T) {
		t.Parallel()
		meta := &symMeta{
			id:      exitTestModule + "/internal/domain/ports.SessionProvider",
			pkgPath: exitTestModule + "/internal/domain/ports",
			name:    "SessionProvider",
			symType: "Type",
		}
		assert.False(t, isPortsInterfaceMeta(meta))
	})

	t.Run("interface outside ports excluded", func(t *testing.T) {
		t.Parallel()
		meta := &symMeta{
			id:              exitTestModule + "/internal/domain/events.Bus",
			pkgPath:         exitTestModule + "/internal/domain/events",
			name:            "Bus",
			symType:         "Type",
			isInterfaceType: true,
		}
		assert.False(t, isPortsInterfaceMeta(meta))
	})

	t.Run("nil meta excluded", func(t *testing.T) {
		t.Parallel()
		assert.False(t, isPortsInterfaceMeta(nil))
	})
}

// TestExitQuery_OrphanReportsUntouched pins the constraint that the exit
// query's deliberate override does not change GatherOrphanReports behavior:
// a zero-consumer ports interface is protected (suppressed) by the orphan
// pipeline while the same symbol surfaces in the exit query.
func TestExitQuery_OrphanReportsUntouched(t *testing.T) {
	t.Parallel()
	root, snap := newExitWorkspace(t)
	health := exitPortsInterface("Health")
	snap.Declarations = []*symMeta{health}

	analyzer := newDeadCodeAnalyzer(&deadCodeSecurityProvider{tempDir: root}, newFixtureIndexer(snap, root))
	ctx := context.Background()

	// The orphan pipeline protects interface contracts: no reports.
	reports, err := analyzer.GatherOrphanReports(ctx, root, false, nil)
	require.NoError(t, err)
	for _, r := range reports {
		assert.NotEqual(t, "Health", r.Symbol, "zero-consumer interface must stay protected in orphan reports")
	}

	// The exit query surfaces it as an orphan candidate.
	candidates, err := analyzer.GatherExitCandidates(ctx, root, nil)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	assert.Equal(t, "Health", candidates[0].Symbol)
	assert.Equal(t, orphanExitLayerLabel, candidates[0].Layer)
	assert.True(t, strings.Contains(FormatExitCandidates(candidates), "Health"))
}
func TestExitQuery_EvaluateUsageLookupError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	state := &scanState{
		targetModule: exitTestModule,
		targetPath:   "/workspace",
	}
	meta := exitPortsInterface("Logger")
	sentinelErr := errors.New("index corruption")

	mockIdx := &mockSymbolIndex{
		GetUsagesFunc: func(ctx context.Context, symbol string, path string, hb chan<- struct{}) ([]location, error) {
			return nil, sentinelErr
		},
	}
	analyzer := &defaultDeadCodeAnalyzer{idx: mockIdx}

	_, ok := analyzer.evaluateExitCandidate(ctx, state, meta, map[string]string{}, nil)
	assert.False(t, ok, "usage lookup failure must skip the symbol")
}

func TestExitQuery_RecordUsageLayer_UnmappedLocation(t *testing.T) {
	t.Parallel()
	analyzer := &defaultDeadCodeAnalyzer{}
	layers := make(map[string]bool)

	// A location with no package mapping is skipped (returns false).
	ok := analyzer.recordExitUsageLayer(
		location{Path: "/unmapped/file.go"},
		map[string]string{},
		exitTestModule,
		layers,
	)
	assert.False(t, ok)
	assert.Empty(t, layers)
}

func TestClassifyExitLayer_InternalWithoutSegment(t *testing.T) {
	t.Parallel()
	// "internal" alone has no layer segment → unknown.
	assert.Equal(t, layerUnknown, classifyExitLayer(exitTestModule+"/internal", exitTestModule))
}

func TestSummarizeExitCandidates(t *testing.T) {
	t.Parallel()

	t.Run("all documented stays summarized", func(t *testing.T) {
		t.Parallel()
		candidates := []ExitCandidate{
			{Symbol: "Capturer", Pkg: exitTestModule + "/internal/domain/ports", Layer: layerApplication, Consumers: 28, Implementers: 416},
			{Symbol: "UIRenderer", Pkg: exitTestModule + "/internal/domain/ports", Layer: layerApplication, Consumers: 5, Implementers: 2},
		}
		out := SummarizeExitCandidates(candidates)

		assert.NotEmpty(t, out)
		// N = 2 for the fixture (Capturer + UIRenderer), both documented stays.
		assert.Contains(t, out, "2 documented stay(s), 0 new candidate(s) requiring adjudication")
		assert.Contains(t, out, "ports governance (ADR-056 Decision 1)")
		assert.Contains(t, out, "-exit-query-verbose")
		// Exactly one trailing newline, no leading blank line.
		assert.Equal(t, 1, strings.Count(out, "\n"))
		assert.False(t, strings.HasPrefix(out, "\n"))
	})

	t.Run("mixed stay and new candidate returns empty", func(t *testing.T) {
		t.Parallel()
		candidates := []ExitCandidate{
			{Symbol: "Capturer", Pkg: exitTestModule + "/internal/domain/ports", Layer: layerApplication},
			{Symbol: "Logger", Pkg: exitTestModule + "/internal/domain/ports", Layer: layerApplication},
		}
		assert.Equal(t, "", SummarizeExitCandidates(candidates))
	})

	t.Run("empty slice returns empty", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "", SummarizeExitCandidates(nil))
		assert.Equal(t, "", SummarizeExitCandidates([]ExitCandidate{}))
	})

	t.Run("single non-stay returns empty", func(t *testing.T) {
		t.Parallel()
		candidates := []ExitCandidate{
			{Symbol: "Logger", Pkg: exitTestModule + "/internal/domain/ports", Layer: layerApplication},
		}
		assert.Equal(t, "", SummarizeExitCandidates(candidates))
	})
}
