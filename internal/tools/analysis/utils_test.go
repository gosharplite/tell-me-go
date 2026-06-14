// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"go/token"
	"go/types"
	"testing"
)

func TestGetSymbolIdentity_NilObj(t *testing.T) {
	got := getSymbolIdentity(nil)
	if got != "" {
		t.Errorf("getSymbolIdentity(nil) = %q, want %q", got, "")
	}
}

func TestGetSymbolIdentity_NoPackage(t *testing.T) {
	// An object whose Pkg() returns nil (e.g., a predeclared type like int)
	obj := types.NewVar(token.NoPos, nil, "MyVar", types.Typ[types.Int])
	got := getSymbolIdentity(obj)
	if got != "MyVar" {
		t.Errorf("getSymbolIdentity(no-pkg obj) = %q, want %q", got, "MyVar")
	}
}

func TestGetSymbolIdentity_PlainFunction(t *testing.T) {
	pkg := types.NewPackage("example.com/mypkg", "mypkg")
	sig := types.NewSignatureType(nil, nil, nil, nil, nil, false)
	fn := types.NewFunc(token.NoPos, pkg, "MyFunc", sig)

	got := getSymbolIdentity(fn)
	want := "example.com/mypkg.MyFunc"
	if got != want {
		t.Errorf("getSymbolIdentity(plain func) = %q, want %q", got, want)
	}
}

func TestGetSymbolIdentity_Method(t *testing.T) {
	pkg := types.NewPackage("example.com/mypkg", "mypkg")

	// Create a named type: type MyType struct{}
	typeName := types.NewTypeName(token.NoPos, pkg, "MyType", nil)
	named := types.NewNamed(typeName, types.NewStruct(nil, nil), nil)

	// Create pointer receiver: *MyType
	ptr := types.NewPointer(named)
	recv := types.NewVar(token.NoPos, nil, "", ptr)

	// Create method signature: func (*MyType) Method()
	sig := types.NewSignatureType(recv, nil, nil, nil, nil, false)
	fn := types.NewFunc(token.NoPos, pkg, "Method", sig)

	got := getSymbolIdentity(fn)
	want := "example.com/mypkg.MyType.Method"
	if got != want {
		t.Errorf("getSymbolIdentity(method) = %q, want %q", got, want)
	}
}

func TestGetSymbolIdentity_TypeParamReceiver(t *testing.T) {
	pkg := types.NewPackage("example.com/mypkg", "mypkg")

	// Create a type parameter T
	tParamName := types.NewTypeName(token.NoPos, pkg, "T", nil)
	tParam := types.NewTypeParam(tParamName, nil)

	// Create receiver of type T
	recv := types.NewVar(token.NoPos, nil, "", tParam)

	// Create method signature: func (T) Method()
	sig := types.NewSignatureType(recv, nil, nil, nil, nil, false)
	fn := types.NewFunc(token.NoPos, pkg, "Method", sig)

	got := getSymbolIdentity(fn)
	want := "example.com/mypkg.T.Method"
	if got != want {
		t.Errorf("getSymbolIdentity(type-param method) = %q, want %q", got, want)
	}
}

func TestGetSymbolIdentity_NonSignatureType(t *testing.T) {
	// A *types.Func whose Type() returns nil hits the !ok fallback.
	pkg := types.NewPackage("example.com/mypkg", "mypkg")
	fn := types.NewFunc(token.NoPos, pkg, "BadFunc", nil)

	got := getSymbolIdentity(fn)
	want := "example.com/mypkg.BadFunc"
	if got != want {
		t.Errorf("getSymbolIdentity(nil-signature func) = %q, want %q", got, want)
	}
}

func TestGetSymbolIdentity_NonNamedNonTypeParamReceiver(t *testing.T) {
	// Receiver type that is neither *types.Named nor *types.TypeParam
	// (e.g., a basic type like int – not valid Go but type-system legal).
	pkg := types.NewPackage("example.com/mypkg", "mypkg")

	recv := types.NewVar(token.NoPos, nil, "", types.Typ[types.Int])
	sig := types.NewSignatureType(recv, nil, nil, nil, nil, false)
	fn := types.NewFunc(token.NoPos, pkg, "Method", sig)

	got := getSymbolIdentity(fn)
	want := "example.com/mypkg.int.Method"
	if got != want {
		t.Errorf("getSymbolIdentity(basic-type method) = %q, want %q", got, want)
	}
}

func TestGetSymbolIdentity_StripsGenericBrackets(t *testing.T) {
	// When a receiver type's String() contains "[", the bracket stripping
	// branch is exercised via the else/TrimPrefix fallback path.
	pkg := types.NewPackage("example.com/mypkg", "mypkg")

	// Use an array type [3]int — String() is "[3]int", so "[" at index 0
	// triggers the bracket-stripping branch, resulting in an empty typeName.
	arrType := types.NewArray(types.Typ[types.Int], 3)
	recv := types.NewVar(token.NoPos, nil, "", arrType)
	sig := types.NewSignatureType(recv, nil, nil, nil, nil, false)
	fn := types.NewFunc(token.NoPos, pkg, "Method", sig)

	got := getSymbolIdentity(fn)
	// TrimPrefix("[3]int", "example.com/mypkg.") → "[3]int" (no change)
	// strings.Index("[3]int", "[") → 0 → typeName = "" (stripped completely)
	want := "example.com/mypkg..Method"
	if got != want {
		t.Errorf("getSymbolIdentity(array-type method) = %q, want %q", got, want)
	}
}

func TestGetBasePkgPath(t *testing.T) {
	tests := []struct{ input, want string }{
		{"example.com/pkg", "example.com/pkg"},
		{"example.com/pkg [some constraint]", "example.com/pkg"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := getBasePkgPath(tt.input); got != tt.want {
			t.Errorf("getBasePkgPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
