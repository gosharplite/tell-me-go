// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"fmt"
	"go/token"
	"go/types"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

// portsTestPkgPath is the module-real ports package path used for all
// fixtures so that isPortsPackagePath matches (the bijection/stay-key inputs
// are documented as already filtered to this package).
const portsTestPkgPath = "github.com/gosharplite/tell-me-go/internal/domain/ports"

// portsInterface builds the symMeta of an interface-type ports export.
func portsInterface(name string) *symMeta {
	return &symMeta{
		id:              portsTestPkgPath + "." + name,
		pkgPath:         portsTestPkgPath,
		name:            name,
		symType:         "Type",
		isInterfaceType: true,
	}
}

// portsNonInterface builds the symMeta of a non-interface ports export of the
// given symType (Type/Function/Constant/Variable).
func portsNonInterface(name, symType string) *symMeta {
	return &symMeta{
		id:      portsTestPkgPath + "." + name,
		pkgPath: portsTestPkgPath,
		name:    name,
		symType: symType,
	}
}

func TestParsePortsRegistry_Conforming(t *testing.T) {
	t.Parallel()

	data := `// # Registry
// ## Family: Alpha
//   - AlphaOne
//   - AlphaTwo
//
// ## Family: Beta
//   - BetaOne
//
// ## Supporting
//   - Helper
//
// All interfaces in this package are designed for dependency injection.
`

	reg, err := parsePortsRegistry(data)
	require.NoError(t, err)
	require.Len(t, reg.Families, 2)

	assert.Equal(t, "Alpha", reg.Families[0].Name)
	assert.Equal(t, []string{"AlphaOne", "AlphaTwo"}, reg.Families[0].Members)

	assert.Equal(t, "Beta", reg.Families[1].Name)
	assert.Equal(t, []string{"BetaOne"}, reg.Families[1].Members)

	assert.Equal(t, []string{"Helper"}, reg.Supporting)
}

func TestParsePortsRegistry_DuplicateFamily(t *testing.T) {
	t.Parallel()

	data := `// # Registry
// ## Family: Alpha
//   - A
// ## Family: Alpha
//   - B
`

	_, err := parsePortsRegistry(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate family")
}

func TestParsePortsRegistry_DuplicateSymbol(t *testing.T) {
	t.Parallel()

	data := `// # Registry
// ## Family: Alpha
//   - Dup
// ## Family: Beta
//   - Dup
`

	_, err := parsePortsRegistry(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate symbol")
}

func TestParsePortsRegistry_BulletBeforeMarker(t *testing.T) {
	t.Parallel()

	data := `// # Registry
//   - Orphan
`

	_, err := parsePortsRegistry(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "before any family or Supporting marker")
}

func TestParsePortsRegistry_BogusDirective(t *testing.T) {
	t.Parallel()

	data := `// # Registry
// ## Bogus
`

	_, err := parsePortsRegistry(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "malformed directive")
}

func TestParsePortsRegistry_EmptyFamilyName(t *testing.T) {
	t.Parallel()

	data := `// # Registry
// ## Family:
`

	_, err := parsePortsRegistry(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty family name")
}

func TestParsePortsRegistry_DuplicateSupporting(t *testing.T) {
	t.Parallel()

	data := `// # Registry
// ## Supporting
// ## Supporting
`

	_, err := parsePortsRegistry(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate Supporting marker")
}

func TestParsePortsRegistry_BlankSeparators(t *testing.T) {
	t.Parallel()

	// Blank `//` lines interspersed between families and around the block
	// must be tolerated (gofmt-stable).
	data := `// # Registry
//
// ## Family: Alpha
//
//   - AlphaOne
//
//
// ## Supporting
//   - Helper
//
`

	reg, err := parsePortsRegistry(data)
	require.NoError(t, err)
	require.Len(t, reg.Families, 1)
	assert.Equal(t, "Alpha", reg.Families[0].Name)
	assert.Equal(t, []string{"AlphaOne"}, reg.Families[0].Members)
	assert.Equal(t, []string{"Helper"}, reg.Supporting)
}

func TestParsePortsRegistry_TrailingProseIgnored(t *testing.T) {
	t.Parallel()

	// The first non-blank, non-directive line terminates the block; anything
	// after it (even text that would look like directives) is prose.
	data := `// # Registry
// ## Family: Alpha
//   - A
// trailing prose
// ## Family: Beta
//   - B
`

	reg, err := parsePortsRegistry(data)
	require.NoError(t, err)
	require.Len(t, reg.Families, 1)
	assert.Equal(t, "Alpha", reg.Families[0].Name)
	assert.Equal(t, []string{"A"}, reg.Families[0].Members)
	assert.Empty(t, reg.Supporting)
}

func TestParsePortsRegistry_LeadingProseIgnored(t *testing.T) {
	t.Parallel()

	data := `// Package ports defines the primary interfaces.
// Some prose before the registry.
// # Registry
// ## Family: Alpha
//   - A
`

	reg, err := parsePortsRegistry(data)
	require.NoError(t, err)
	require.Len(t, reg.Families, 1)
	assert.Equal(t, "Alpha", reg.Families[0].Name)
	assert.Equal(t, []string{"A"}, reg.Families[0].Members)
}

func TestVerifyPortsRegistryBijection_Phantom(t *testing.T) {
	t.Parallel()

	reg := &portsRegistry{
		Families: []portsRegistryFamily{
			{Name: "Alpha", Members: []string{"AlphaOne", "Ghost"}},
		},
	}
	decls := []*symMeta{portsInterface("AlphaOne")}

	violations := verifyPortsRegistryBijection(reg, decls)
	require.Len(t, violations, 1)
	assert.Contains(t, violations[0], "phantom")
	assert.Contains(t, violations[0], "Ghost")
}

func TestVerifyPortsRegistryBijection_Omission(t *testing.T) {
	t.Parallel()

	reg := &portsRegistry{
		Families: []portsRegistryFamily{
			{Name: "Alpha", Members: []string{"AlphaOne"}},
		},
	}
	decls := []*symMeta{
		portsInterface("AlphaOne"),
		portsInterface("Missing"),
	}

	violations := verifyPortsRegistryBijection(reg, decls)
	require.Len(t, violations, 1)
	assert.Contains(t, violations[0], "omission")
	assert.Contains(t, violations[0], "Missing")
}

func TestVerifyPortsRegistryBijection_Misclassification(t *testing.T) {
	t.Parallel()

	t.Run("non-interface in a family", func(t *testing.T) {
		t.Parallel()
		reg := &portsRegistry{
			Families: []portsRegistryFamily{
				{Name: "Alpha", Members: []string{"NotAnInterface"}},
			},
		}
		decls := []*symMeta{portsNonInterface("NotAnInterface", "Type")}

		violations := verifyPortsRegistryBijection(reg, decls)
		require.Len(t, violations, 1)
		assert.Contains(t, violations[0], "misclassification")
		assert.Contains(t, violations[0], "not an interface")
	})

	t.Run("interface in Supporting", func(t *testing.T) {
		t.Parallel()
		reg := &portsRegistry{Supporting: []string{"IsInterface"}}
		decls := []*symMeta{portsInterface("IsInterface")}

		violations := verifyPortsRegistryBijection(reg, decls)
		require.Len(t, violations, 1)
		assert.Contains(t, violations[0], "misclassification")
		assert.Contains(t, violations[0], "is an interface")
	})
}

func TestVerifyPortsRegistryBijection_CrossBucketDuplicate(t *testing.T) {
	t.Parallel()

	reg := &portsRegistry{
		Families: []portsRegistryFamily{
			{Name: "Alpha", Members: []string{"Dup"}},
			{Name: "Beta", Members: []string{"Dup"}},
		},
	}
	decls := []*symMeta{portsInterface("Dup")}

	violations := verifyPortsRegistryBijection(reg, decls)
	require.Len(t, violations, 1)
	assert.Contains(t, violations[0], "duplicate")
	assert.Contains(t, violations[0], "Dup")
}

func TestVerifyPortsRegistryBijection_ExactMatch(t *testing.T) {
	t.Parallel()

	reg := &portsRegistry{
		Families: []portsRegistryFamily{
			{Name: "Alpha", Members: []string{"AlphaOne", "AlphaTwo"}},
			{Name: "Beta", Members: []string{"BetaOne"}},
		},
		Supporting: []string{"Helper"},
	}
	decls := []*symMeta{
		portsInterface("AlphaOne"),
		portsInterface("AlphaTwo"),
		portsInterface("BetaOne"),
		portsNonInterface("Helper", "Variable"),
	}

	assert.Empty(t, verifyPortsRegistryBijection(reg, decls))
}

func registryWithNFamilies(n int) *portsRegistry {
	reg := &portsRegistry{}
	for i := 0; i < n; i++ {
		reg.Families = append(reg.Families, portsRegistryFamily{Name: fmt.Sprintf("Family%02d", i)})
	}
	return reg
}

func TestVerifyPortsRegistryFamilyBound(t *testing.T) {
	t.Parallel()

	assert.Empty(t, verifyPortsRegistryFamilyBound(registryWithNFamilies(12)))

	violations := verifyPortsRegistryFamilyBound(registryWithNFamilies(13))
	require.Len(t, violations, 1)
	assert.Contains(t, violations[0], "13")
	assert.Contains(t, violations[0], "12")
}

func TestVerifyPortsRegistryStayKeyLiveness(t *testing.T) {
	t.Parallel()

	keys := make([]string, 0, len(exitStayRationales))
	for k := range exitStayRationales {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	require.Len(t, keys, 9)

	allPresent := func() []*symMeta {
		decls := make([]*symMeta, 0, len(keys))
		for _, k := range keys {
			decls = append(decls, portsInterface(k))
		}
		return decls
	}

	// All seven stay keys present as ports interface decls → pass.
	assert.Empty(t, verifyPortsRegistryStayKeyLiveness(allPresent()))

	// Drop the last key and confirm the violation names it.
	dropped := keys[len(keys)-1]
	decls := make([]*symMeta, 0, len(keys)-1)
	for _, k := range keys[:len(keys)-1] {
		decls = append(decls, portsInterface(k))
	}
	violations := verifyPortsRegistryStayKeyLiveness(decls)
	require.Len(t, violations, 1)
	assert.Contains(t, violations[0], dropped)
}

// portsTestPackage returns a fresh ports package whose path matches
// isPortsPackagePath, so synthetic named types are treated as ports-declared.
func portsTestPackage() *types.Package {
	return types.NewPackage(portsTestPkgPath, "ports")
}

// portsTypeEntry builds a non-interface Type symMeta with a real object.
func portsTypeEntry(name string, obj types.Object) *symMeta {
	return &symMeta{
		id:      portsTestPkgPath + "." + name,
		pkgPath: portsTestPkgPath,
		name:    name,
		symType: "Type",
		obj:     obj,
	}
}

// portsFuncEntry builds a non-interface Function symMeta with a real object.
func portsFuncEntry(name string, obj types.Object) *symMeta {
	return &symMeta{
		id:      portsTestPkgPath + "." + name,
		pkgPath: portsTestPkgPath,
		name:    name,
		symType: "Function",
		obj:     obj,
	}
}

func TestIsPortsDeclaredType(t *testing.T) {
	t.Parallel()

	pkg := portsTestPackage()
	structType := types.NewStruct(nil, nil)
	portsNamed := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Widget", structType), structType, nil)

	otherNamed := types.NewNamed(
		types.NewTypeName(token.NoPos, types.NewPackage("example.com/other", "other"), "Thing", structType),
		structType,
		nil,
	)

	assert.True(t, isPortsDeclaredType(portsNamed))
	assert.True(t, isPortsDeclaredType(types.NewPointer(portsNamed))) // pointer deref
	assert.False(t, isPortsDeclaredType(otherNamed))
	assert.False(t, isPortsDeclaredType(types.Typ[types.String])) // non-named
	assert.False(t, isPortsDeclaredType(nil))
}

func TestReferencesPortsType(t *testing.T) {
	t.Parallel()

	pkg := portsTestPackage()
	portsStruct := types.NewStruct(nil, nil)
	portsNamed := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Widget", portsStruct), portsStruct, nil)

	t.Run("struct field of ports type", func(t *testing.T) {
		t.Parallel()
		container := types.NewStruct([]*types.Var{
			types.NewVar(token.NoPos, pkg, "W", portsNamed),
		}, nil)
		assert.True(t, referencesPortsType(container))
	})

	t.Run("signature param and result of ports type", func(t *testing.T) {
		t.Parallel()
		sig := types.NewSignatureType(nil, nil, nil,
			types.NewTuple(types.NewVar(token.NoPos, pkg, "w", portsNamed)),
			types.NewTuple(types.NewVar(token.NoPos, pkg, "", portsNamed)),
			false)
		assert.True(t, referencesPortsType(sig))
	})

	t.Run("pointer to ports type", func(t *testing.T) {
		t.Parallel()
		assert.True(t, referencesPortsType(types.NewPointer(portsNamed)))
	})

	t.Run("map key and slice elem", func(t *testing.T) {
		t.Parallel()
		assert.True(t, referencesPortsType(types.NewMap(portsNamed, types.Typ[types.Int])))
		assert.True(t, referencesPortsType(types.NewSlice(portsNamed)))
	})

	t.Run("all stdlib", func(t *testing.T) {
		t.Parallel()
		sig := types.NewSignatureType(nil, nil, nil,
			types.NewTuple(types.NewVar(token.NoPos, nil, "s", types.Typ[types.String])),
			types.NewTuple(types.NewVar(token.NoPos, nil, "", types.Typ[types.Int])),
			false)
		assert.False(t, referencesPortsType(sig))
	})

	t.Run("terminates at non-ports named type", func(t *testing.T) {
		t.Parallel()
		// outerNamed wraps a struct containing a ports field, but outerNamed
		// is a non-ports type: the walker must terminate and report false.
		innerStruct := types.NewStruct([]*types.Var{
			types.NewVar(token.NoPos, pkg, "W", portsNamed),
		}, nil)
		outerNamed := types.NewNamed(
			types.NewTypeName(token.NoPos, types.NewPackage("example.com/other", "other"), "Outer", innerStruct),
			innerStruct,
			nil,
		)
		container := types.NewStruct([]*types.Var{
			types.NewVar(token.NoPos, nil, "O", outerNamed),
		}, nil)
		assert.False(t, referencesPortsType(container))
	})
}

func TestEvaluateSupportingClauses_ClauseB(t *testing.T) {
	t.Parallel()

	nonInterfaces := []*symMeta{
		portsNonInterface("Const1", "Constant"),
		portsNonInterface("Var1", "Variable"),
		portsFuncEntry("FormatLine", types.NewFunc(token.NoPos, nil, "FormatLine",
			types.NewSignatureType(nil, nil, nil,
				types.NewTuple(types.NewVar(token.NoPos, nil, "s", types.Typ[types.String])),
				nil, false))),
	}

	admitted := evaluateSupportingClauses(nil, nonInterfaces)

	assert.Equal(t, "b", admitted["Const1"])
	assert.Equal(t, "b", admitted["Var1"])
	_, ok := admitted["FormatLine"]
	assert.False(t, ok, "Function with stdlib-only signature must not be admitted by (b)")
}

func TestEvaluateSupportingClauses_ClauseC(t *testing.T) {
	t.Parallel()

	pkg := portsTestPackage()
	portsStruct := types.NewStruct(nil, nil)
	portsNamed := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Widget", portsStruct), portsStruct, nil)

	portsParamFn := types.NewFunc(token.NoPos, pkg, "FormatLine",
		types.NewSignatureType(nil, nil, nil,
			types.NewTuple(types.NewVar(token.NoPos, pkg, "w", portsNamed)),
			nil, false))

	stdlibFn := types.NewFunc(token.NoPos, pkg, "FormatFinalLine",
		types.NewSignatureType(nil, nil, nil,
			types.NewTuple(types.NewVar(token.NoPos, nil, "s", types.Typ[types.String])),
			types.NewTuple(types.NewVar(token.NoPos, nil, "", types.Typ[types.Int])),
			false))

	admitted := evaluateSupportingClauses(nil, []*symMeta{
		portsFuncEntry("FormatLine", portsParamFn),
		portsFuncEntry("FormatFinalLine", stdlibFn),
	})

	assert.Equal(t, "c", admitted["FormatLine"])
	_, ok := admitted["FormatFinalLine"]
	assert.False(t, ok, "all-stdlib signature must not be admitted by (c)")
}

func TestEvaluateSupportingClauses_ClauseD(t *testing.T) {
	t.Parallel()

	pkg := portsTestPackage()

	renderIface := types.NewInterfaceType([]*types.Func{
		types.NewFunc(token.NoPos, pkg, "Render",
			types.NewSignatureType(nil, nil, nil, nil, nil, false)),
	}, nil)

	structType := types.NewStruct(nil, nil)
	implNamed := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "RendererImpl", nil), structType, nil)
	recv := types.NewVar(token.NoPos, pkg, "r", implNamed)
	implNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "Render",
		types.NewSignatureType(recv, nil, nil, nil, nil, false)))

	admitted := evaluateSupportingClauses(
		map[string]*types.Interface{"Renderer": renderIface},
		[]*symMeta{portsTypeEntry("RendererImpl", implNamed.Obj())},
	)

	assert.Equal(t, "d", admitted["RendererImpl"])
}

func TestEvaluateSupportingClauses_ClauseA(t *testing.T) {
	t.Parallel()

	pkg := portsTestPackage()

	// Widget is referenced only by Helper's field.
	widgetStruct := types.NewStruct(nil, nil)
	widgetNamed := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Widget", nil), widgetStruct, nil)

	// Helper is referenced by the ports interface's method param.
	helperStruct := types.NewStruct([]*types.Var{
		types.NewVar(token.NoPos, pkg, "W", widgetNamed),
	}, nil)
	helperNamed := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Helper", nil), helperStruct, nil)

	renderIface := types.NewInterfaceType([]*types.Func{
		types.NewFunc(token.NoPos, pkg, "Render",
			types.NewSignatureType(nil, nil, nil,
				types.NewTuple(types.NewVar(token.NoPos, pkg, "h", helperNamed)),
				nil, false)),
	}, nil)

	admitted := evaluateSupportingClauses(
		map[string]*types.Interface{"Renderer": renderIface},
		[]*symMeta{
			portsTypeEntry("Helper", helperNamed.Obj()),
			portsTypeEntry("Widget", widgetNamed.Obj()),
		},
	)

	// Helper is a param of the ports interface → admitted by (a) seed.
	assert.Equal(t, "a", admitted["Helper"])
	// Widget is a field of the already-admitted Helper → admitted by (a) interleaving.
	assert.Equal(t, "a", admitted["Widget"])
}

func TestVerifyPortsSupportingAdmission_ExpelPath(t *testing.T) {
	t.Parallel()

	stdlibFn := types.NewFunc(token.NoPos, nil, "FormatFinalLine",
		types.NewSignatureType(nil, nil, nil,
			types.NewTuple(types.NewVar(token.NoPos, nil, "s", types.Typ[types.String])),
			types.NewTuple(types.NewVar(token.NoPos, nil, "", types.Typ[types.Int])),
			false))

	decls := []*symMeta{
		{
			id:      portsTestPkgPath + ".FormatFinalLine",
			pkgPath: portsTestPkgPath,
			name:    "FormatFinalLine",
			symType: "Function",
			obj:     stdlibFn,
		},
	}

	reg := &portsRegistry{Supporting: []string{"FormatFinalLine"}}
	state := &scanState{pkgs: []*packages.Package{}}
	analyzer := &defaultDeadCodeAnalyzer{idx: &mockSymbolIndex{}}

	violations := analyzer.verifyPortsSupportingAdmission(reg, decls, state, nil)

	require.Len(t, violations, 1)
	assert.Contains(t, violations[0], "supporting entry")
	assert.Contains(t, violations[0], "FormatFinalLine")
	assert.Contains(t, violations[0], "not admitted by any clause")
}

func TestVerifyPortsSupportingAdmission_Admitted(t *testing.T) {
	t.Parallel()

	decls := []*symMeta{
		{
			id:      portsTestPkgPath + ".LineSep",
			pkgPath: portsTestPkgPath,
			name:    "LineSep",
			symType: "Constant",
		},
	}

	reg := &portsRegistry{Supporting: []string{"LineSep"}}
	state := &scanState{pkgs: []*packages.Package{}}
	analyzer := &defaultDeadCodeAnalyzer{idx: &mockSymbolIndex{}}

	assert.Empty(t, analyzer.verifyPortsSupportingAdmission(reg, decls, state, nil))
}

func TestEvaluateSupportingClauses_ClauseA_FuncType(t *testing.T) {
	t.Parallel()

	pkg := portsTestPackage()

	// Widget is referenced only by a Supporting func type's param.
	widgetStruct := types.NewStruct(nil, nil)
	widgetNamed := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Widget", nil), widgetStruct, nil)

	// RenderFn is a func-type declaration (Type entry) whose signature
	// references the ports-declared Widget type.
	renderFnSig := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(types.NewVar(token.NoPos, pkg, "w", widgetNamed)),
		nil, false)
	renderFnNamed := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "RenderFn", nil), renderFnSig, nil)

	admitted := evaluateSupportingClauses(
		nil,
		[]*symMeta{
			portsTypeEntry("RenderFn", renderFnNamed.Obj()),
			portsTypeEntry("Widget", widgetNamed.Obj()),
		},
	)

	// RenderFn is admitted by (c): its signature references a ports type.
	assert.Equal(t, "c", admitted["RenderFn"])
	// Widget is a param of the admitted RenderFn → admitted by (a).
	assert.Equal(t, "a", admitted["Widget"])
}
