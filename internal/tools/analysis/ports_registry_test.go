// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.Len(t, keys, 6)

	allPresent := func() []*symMeta {
		decls := make([]*symMeta, 0, len(keys))
		for _, k := range keys {
			decls = append(decls, portsInterface(k))
		}
		return decls
	}

	// All six stay keys present as ports interface decls → pass.
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
