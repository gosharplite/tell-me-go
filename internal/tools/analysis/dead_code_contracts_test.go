// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"go/token"
	"go/types"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

// makeSig builds a *types.Signature from named basic-kind tokens. Supported tokens:
//
//	"string", "int", "int64", "rune", "error", "[]byte", "any" (interface{}),
//	"interface{}".
//
// Anything else triggers t.Fatalf.
func makeSig(t *testing.T, params, results []string) *types.Signature {
	t.Helper()
	mkType := func(tok string) types.Type {
		switch tok {
		case "string":
			return types.Typ[types.String]
		case "int":
			return types.Typ[types.Int]
		case "int64":
			return types.Typ[types.Int64]
		case "rune":
			return types.Typ[types.Rune]
		case "error":
			return types.Universe.Lookup("error").Type()
		case "[]byte":
			return types.NewSlice(types.Typ[types.Byte])
		case "any":
			return types.Universe.Lookup("any").Type()
		case "interface{}":
			i := types.NewInterfaceType(nil, nil)
			i.Complete()
			return i
		default:
			t.Fatalf("makeSig: unsupported token %q", tok)
			return nil
		}
	}
	mkTuple := func(toks []string) *types.Tuple {
		vars := make([]*types.Var, len(toks))
		for i, tok := range toks {
			vars[i] = types.NewVar(token.NoPos, nil, "", mkType(tok))
		}
		return types.NewTuple(vars...)
	}
	return types.NewSignatureType(nil, nil, nil, mkTuple(params), mkTuple(results), false)
}

// TestIsWellKnownContract_Table covers the public API of isWellKnownContract
// with synthetic signatures built from basic kinds. Real-stdlib types
// (io.Writer, *http.Request, etc.) are exercised separately by
// TestIsWellKnownContract_RealStdlib so that this table-driven test stays
// fast and self-contained.
func TestIsWellKnownContract_Table(t *testing.T) {
	t.Parallel()
	a := &defaultDeadCodeAnalyzer{}

	type tc struct {
		name    string
		method  string
		params  []string
		results []string
		want    bool
	}
	cases := []tc{
		// --- Positive: legacy contracts ---
		{"Error()string", "Error", nil, []string{"string"}, true},
		{"String()string", "String", nil, []string{"string"}, true},
		// --- Positive: io contracts (basic-kind-only) ---
		{"Read([]byte)(int,error)", "Read", []string{"[]byte"}, []string{"int", "error"}, true},
		{"Write([]byte)(int,error)", "Write", []string{"[]byte"}, []string{"int", "error"}, true},
		{"Close()error", "Close", nil, []string{"error"}, true},
		{"Seek(int64,int)(int64,error)", "Seek", []string{"int64", "int"}, []string{"int64", "error"}, true},
		// --- Positive: encoding/json + encoding ---
		{"MarshalJSON()([]byte,error)", "MarshalJSON", nil, []string{"[]byte", "error"}, true},
		{"UnmarshalJSON([]byte)error", "UnmarshalJSON", []string{"[]byte"}, []string{"error"}, true},
		{"MarshalText()([]byte,error)", "MarshalText", nil, []string{"[]byte", "error"}, true},
		{"UnmarshalText([]byte)error", "UnmarshalText", []string{"[]byte"}, []string{"error"}, true},
		{"MarshalBinary()([]byte,error)", "MarshalBinary", nil, []string{"[]byte", "error"}, true},
		{"UnmarshalBinary([]byte)error", "UnmarshalBinary", []string{"[]byte"}, []string{"error"}, true},
		// --- Positive: database/sql.Scanner with both alias spellings ---
		{"Scan(any)error", "Scan", []string{"any"}, []string{"error"}, true},
		{"Scan(interface{})error", "Scan", []string{"interface{}"}, []string{"error"}, true},

		// --- Negative: matching name, wrong signature ---
		{"Write(string)", "Write", []string{"string"}, nil, false},
		{"Close()(error,error)", "Close", nil, []string{"error", "error"}, false},
		{"Read()", "Read", nil, nil, false},
		{"MarshalJSON([]byte)error", "MarshalJSON", []string{"[]byte"}, []string{"error"}, false},
		{"Seek(int,int)(int64,error)", "Seek", []string{"int", "int"}, []string{"int64", "error"}, false},
		{"Scan()error", "Scan", nil, []string{"error"}, false},

		// --- Negative: unknown method names ---
		{"Foo()string", "Foo", nil, []string{"string"}, false},
		{"Bar([]byte)error", "Bar", []string{"[]byte"}, []string{"error"}, false},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			sig := makeSig(t, c.params, c.results)
			fn := types.NewFunc(token.NoPos, nil, c.method, sig)
			got := a.isWellKnownContract(fn)
			assert.Equal(t, c.want, got,
				"isWellKnownContract(%s with params=%v results=%v) = %v; want %v",
				c.method, c.params, c.results, got, c.want)
		})
	}
}

// TestIsWellKnownContract_RealStdlib loads actual standard-library packages
// and asserts that every method on every well-known interface is recognized
// by isWellKnownContract. This guards against drift between the table in
// dead_code.go and the real stdlib (e.g., a Go release renaming a parameter
// would not break us because we compare types, not names — but a signature
// change WOULD, and we want to know).
//
// Skipped in -short mode because it loads stdlib packages.
func TestIsWellKnownContract_RealStdlib(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stdlib load in -short mode")
	}
	t.Parallel()

	// Map of stdlib package path -> single-method interfaces we expect to protect.
	want := map[string][]string{
		"io":            {"Reader", "Writer", "Closer", "WriterTo", "ReaderFrom", "Seeker"},
		"encoding/json": {"Marshaler", "Unmarshaler"},
		"encoding":      {"TextMarshaler", "TextUnmarshaler", "BinaryMarshaler", "BinaryUnmarshaler"},
		"net/http":      {"Handler"},
		"database/sql":  {"Scanner"},
		"fmt":           {"Stringer", "Formatter"},
	}

	pkgPaths := make([]string, 0, len(want))
	for p := range want {
		pkgPaths = append(pkgPaths, p)
	}

	cfg := &packages.Config{
		Mode: packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax |
			packages.NeedDeps | packages.NeedImports | packages.NeedName,
	}
	pkgs, err := packages.Load(cfg, pkgPaths...)
	require.NoError(t, err)

	a := &defaultDeadCodeAnalyzer{}

	for _, pkg := range pkgs {
		ifaceNames, ok := want[pkg.PkgPath]
		if !ok {
			continue
		}
		for _, ifaceName := range ifaceNames {
			obj := pkg.Types.Scope().Lookup(ifaceName)
			require.NotNilf(t, obj, "stdlib %s.%s not found", pkg.PkgPath, ifaceName)
			itf, ok := obj.Type().Underlying().(*types.Interface)
			require.Truef(t, ok, "%s.%s is not an interface", pkg.PkgPath, ifaceName)
			for i := 0; i < itf.NumMethods(); i++ {
				m := itf.Method(i)
				assert.Truef(t, a.isWellKnownContract(m),
					"isWellKnownContract should accept %s.%s.%s (sig: %s)",
					pkg.PkgPath, ifaceName, m.Name(), m.Type())
			}
		}
	}
}

// TestIsWellKnownContract_Negative_Var keeps the existing guard for
// non-function objects working after the structural-matcher refactor.
func TestIsWellKnownContract_Negative_Var(t *testing.T) {
	t.Parallel()
	a := &defaultDeadCodeAnalyzer{}
	v := types.NewVar(token.NoPos, nil, "X", types.Typ[types.Int])
	assert.False(t, a.isWellKnownContract(v))
}

// TestCanonicalTypeString_Aliases documents the alias normalizations that
// allow user-written variants to match the stdlib-derived table:
//   - `any` and `interface{}` both collapse to `any` (Scanner.Scan).
//   - `[]uint8` and `[]byte` both collapse to `[]byte` (every io/encoding
//     contract that takes or returns a byte slice).
func TestCanonicalTypeString_Aliases(t *testing.T) {
	t.Parallel()
	anyT := types.Universe.Lookup("any").Type()
	emptyIface := types.NewInterfaceType(nil, nil)
	emptyIface.Complete()

	assert.Equal(t, "any", canonicalTypeString(anyT))
	assert.Equal(t, "any", canonicalTypeString(emptyIface),
		"empty interface{} should normalize to any")

	byteSlice := types.NewSlice(types.Typ[types.Byte])
	uint8Slice := types.NewSlice(types.Typ[types.Uint8])
	assert.Equal(t, "[]byte", canonicalTypeString(byteSlice),
		"NewSlice(Byte) should normalize to []byte")
	assert.Equal(t, "[]byte", canonicalTypeString(uint8Slice),
		"NewSlice(Uint8) should normalize to []byte")

	// Sanity: a non-aliased type is unaffected.
	assert.Equal(t, "string", canonicalTypeString(types.Typ[types.String]))
}
