// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"go/ast"
	"go/token"
	"go/types"
	"sync"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestSequenceVisitor_Visit_NilNode(t *testing.T) {
	t.Parallel()
	v := &sequenceVisitor{
		ctx:      context.Background(),
		analyzer: &defaultSequenceAnalyzer{},
	}
	if got := v.Visit(nil); got != nil {
		t.Errorf("Visit(nil) = %v, want nil", got)
	}
}

func TestSequenceVisitor_Visit_CancelledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	v := &sequenceVisitor{
		ctx:      ctx,
		analyzer: &defaultSequenceAnalyzer{},
	}
	if got := v.Visit(&ast.Ident{Name: "x"}); got != nil {
		t.Errorf("Visit with cancelled ctx = %v, want nil", got)
	}
}

func TestSequenceVisitor_Visit_ForStmt(t *testing.T) {
	t.Parallel()
	_, f := parseTestFile(t, "package p; func f() { for i := 0; i < 10; i++ { g() } }")
	fd := f.Decls[0].(*ast.FuncDecl)
	forStmt := fd.Body.List[0].(*ast.ForStmt)

	pkg := &packages.Package{
		Name:    "p",
		PkgPath: "test/pkg",
		TypesInfo: &types.Info{
			Uses: make(map[*ast.Ident]types.Object),
			Defs: make(map[*ast.Ident]types.Object),
		},
	}

	frames := &frameCollector{}
	v := &sequenceVisitor{
		ctx:      context.Background(),
		pkg:      pkg,
		analyzer: &defaultSequenceAnalyzer{},
		frames:   frames,
		maxDepth: 1,
	}

	got := v.Visit(forStmt)
	if got != nil {
		t.Errorf("Visit(ForStmt) = %v, want nil", got)
	}
	if v.inLoop != 0 {
		t.Errorf("inLoop = %d, want 0 (restored after ForStmt)", v.inLoop)
	}
}

func TestSequenceVisitor_Visit_RangeStmt(t *testing.T) {
	t.Parallel()
	_, f := parseTestFile(t, "package p; func f() { for _, x := range items { h() } }")
	fd := f.Decls[0].(*ast.FuncDecl)
	rangeStmt := fd.Body.List[0].(*ast.RangeStmt)

	pkg := &packages.Package{
		Name:    "p",
		PkgPath: "test/pkg",
		TypesInfo: &types.Info{
			Uses: make(map[*ast.Ident]types.Object),
			Defs: make(map[*ast.Ident]types.Object),
		},
	}

	frames := &frameCollector{}
	v := &sequenceVisitor{
		ctx:      context.Background(),
		pkg:      pkg,
		analyzer: &defaultSequenceAnalyzer{},
		frames:   frames,
		maxDepth: 1,
	}

	got := v.Visit(rangeStmt)
	if got != nil {
		t.Errorf("Visit(RangeStmt) = %v, want nil", got)
	}
	if v.inLoop != 0 {
		t.Errorf("inLoop = %d, want 0 (restored after RangeStmt)", v.inLoop)
	}
}

func TestSequenceVisitor_Visit_GoStmt(t *testing.T) {
	t.Parallel()
	_, f := parseTestFile(t, "package p; func f() { go async() }")
	fd := f.Decls[0].(*ast.FuncDecl)
	goStmt := fd.Body.List[0].(*ast.GoStmt)

	pkg := &packages.Package{
		Name:    "p",
		PkgPath: "test/pkg",
		TypesInfo: &types.Info{
			Uses: make(map[*ast.Ident]types.Object),
			Defs: make(map[*ast.Ident]types.Object),
		},
	}

	frames := &frameCollector{}
	v := &sequenceVisitor{
		ctx:      context.Background(),
		pkg:      pkg,
		analyzer: &defaultSequenceAnalyzer{},
		frames:   frames,
		maxDepth: 1,
	}

	got := v.Visit(goStmt)
	if got != nil {
		t.Errorf("Visit(GoStmt) = %v, want nil", got)
	}
	if v.inGo {
		t.Error("inGo = true, want false (restored after GoStmt)")
	}
}

func TestSequenceVisitor_Visit_CallExpr(t *testing.T) {
	t.Parallel()
	pkgA, _ := setupMockPackages()

	var callExpr *ast.CallExpr
	for _, decl := range pkgA.Syntax[0].Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name.Name == "StartFunc" {
			callExpr = fd.Body.List[0].(*ast.ExprStmt).X.(*ast.CallExpr)
			break
		}
	}
	if callExpr == nil {
		t.Fatal("CallExpr not found in StartFunc")
	}

	frames := &frameCollector{}
	v := &sequenceVisitor{
		ctx:      context.Background(),
		pkg:      pkgA,
		modName:  "github.com/test/mod",
		analyzer: &defaultSequenceAnalyzer{modName: "github.com/test/mod"},
		frames:   frames,
		visited:  make(map[string]bool),
		maxDepth: 1,
	}

	got := v.Visit(callExpr)
	if got != v {
		t.Errorf("Visit(CallExpr) = %v, want self (%v)", got, v)
	}
	if len(frames.frames) == 0 {
		t.Error("expected at least one frame from CallExpr")
	}
}

func TestSequenceVisitor_Visit_UnhandledNode(t *testing.T) {
	t.Parallel()
	v := &sequenceVisitor{
		ctx:      context.Background(),
		analyzer: &defaultSequenceAnalyzer{},
	}
	got := v.Visit(&ast.BasicLit{Kind: token.INT, Value: "0"})
	if got != v {
		t.Errorf("Visit(BasicLit) = %v, want self (%v)", got, v)
	}
}

// TestSequenceVisitor_HandleCall exercises all uncovered branches in handleCall
// via table-driven subtests that each call handleCall directly and assert
// side effects on v.frames.frames.
func TestSequenceVisitor_HandleCall(t *testing.T) {
	t.Parallel()

	t.Run("non-ident non-selector call returns early", func(t *testing.T) {
		t.Parallel()
		// Parse: package p; func f() { func(){}() }
		// The outer CallExpr has Fun = *ast.FuncLit → hits default case.
		_, f := parseTestFile(t, "package p; func f() { func(){}() }")
		fd := f.Decls[0].(*ast.FuncDecl)
		exprStmt := fd.Body.List[0].(*ast.ExprStmt)
		call := exprStmt.X.(*ast.CallExpr)

		v := &sequenceVisitor{
			ctx: context.Background(),
			pkg: &packages.Package{
				PkgPath:   "example.com/local/pkg",
				TypesInfo: &types.Info{Uses: make(map[*ast.Ident]types.Object)},
			},
			analyzer: &defaultSequenceAnalyzer{},
			frames:   &frameCollector{},
			modName:  "example.com/local",
		}

		v.handleCall(call)
		if len(v.frames.frames) != 0 {
			t.Errorf("expected 0 frames, got %d", len(v.frames.frames))
		}
	})

	t.Run("empty selector name returns early", func(t *testing.T) {
		t.Parallel()
		// Manually construct a CallExpr whose Fun is a SelectorExpr with an
		// empty Sel.Name. resolveSelectorCall returns targetFunc="" → early
		// return via targetFunc=="" guard.
		call := &ast.CallExpr{
			Fun: &ast.SelectorExpr{
				X:   &ast.Ident{Name: "x"},
				Sel: &ast.Ident{Name: ""},
			},
		}

		v := &sequenceVisitor{
			ctx: context.Background(),
			pkg: &packages.Package{
				PkgPath:   "example.com/local/pkg",
				TypesInfo: &types.Info{Uses: make(map[*ast.Ident]types.Object)},
			},
			analyzer: &defaultSequenceAnalyzer{},
			frames:   &frameCollector{},
			modName:  "example.com/local",
		}

		v.handleCall(call)
		if len(v.frames.frames) != 0 {
			t.Errorf("expected 0 frames, got %d", len(v.frames.frames))
		}
	})

	t.Run("external package call returns early", func(t *testing.T) {
		t.Parallel()
		_, f := parseTestFile(t, `package p
import "fmt"
func f() { fmt.Println() }`)
		fd := f.Decls[1].(*ast.FuncDecl)
		exprStmt := fd.Body.List[0].(*ast.ExprStmt)
		call := exprStmt.X.(*ast.CallExpr)

		sel := call.Fun.(*ast.SelectorExpr)
		fmtIdent := sel.X.(*ast.Ident) // "fmt" identifier
		printlnIdent := sel.Sel        // "Println" identifier

		// Build the "fmt" standard library package.
		fmtPkg := types.NewPackage("fmt", "fmt")
		pkgTypes := types.NewPackage("example.com/local/pkg", "p")

		// The PkgName representing the import statement.
		pkgName := types.NewPkgName(token.NoPos, pkgTypes, "fmt", fmtPkg)

		// Println function in the fmt package.
		// The signature is irrelevant for this test — we only need the
		// object to exist so resolveSelectorCall can map targetPkgPath.
		printlnSig := types.NewSignatureType(nil, nil, nil, nil, nil, false)
		printlnFunc := types.NewFunc(token.NoPos, fmtPkg, "Println", printlnSig)

		uses := make(map[*ast.Ident]types.Object)
		uses[fmtIdent] = pkgName
		uses[printlnIdent] = printlnFunc

		v := &sequenceVisitor{
			ctx: context.Background(),
			pkg: &packages.Package{
				PkgPath:   "example.com/local/pkg",
				Types:     pkgTypes,
				TypesInfo: &types.Info{Uses: uses},
			},
			analyzer: &defaultSequenceAnalyzer{},
			frames:   &frameCollector{},
			modName:  "example.com/local",
		}

		// handleCall resolves fmt.Println → targetPkgPath="fmt".
		// isInternal("fmt") returns false because "fmt" does not have
		// prefix "example.com/local".
		v.handleCall(call)
		if len(v.frames.frames) != 0 {
			t.Errorf("expected 0 frames, got %d", len(v.frames.frames))
		}
	})

	t.Run("duplicate consecutive call skipped", func(t *testing.T) {
		t.Parallel()
		_, f := parseTestFile(t, "package p; func f() { g(); g() }")
		fd := f.Decls[0].(*ast.FuncDecl)
		call1 := fd.Body.List[0].(*ast.ExprStmt).X.(*ast.CallExpr)
		call2 := fd.Body.List[1].(*ast.ExprStmt).X.(*ast.CallExpr)

		ident1 := call1.Fun.(*ast.Ident)
		ident2 := call2.Fun.(*ast.Ident)

		pkgPath := "example.com/local/pkg"
		pkgTypes := types.NewPackage(pkgPath, "p")

		gFunc := types.NewFunc(token.NoPos, pkgTypes, "g",
			types.NewSignatureType(nil, nil, nil, nil, nil, false))

		uses := make(map[*ast.Ident]types.Object)
		uses[ident1] = gFunc
		uses[ident2] = gFunc

		defs := make(map[*ast.Ident]types.Object)
		defs[fd.Name] = types.NewFunc(token.NoPos, pkgTypes, "f",
			types.NewSignatureType(nil, nil, nil, nil, nil, false))

		v := &sequenceVisitor{
			ctx: context.Background(),
			pkg: &packages.Package{
				PkgPath:   pkgPath,
				Types:     pkgTypes,
				TypesInfo: &types.Info{Uses: uses, Defs: defs},
			},
			analyzer: &defaultSequenceAnalyzer{},
			frames:   &frameCollector{},
			modName:  "example.com/local",
			depth:    0,
			maxDepth: 1, // prevents tryRecurse from doing real work
		}

		// First call adds a frame.
		v.handleCall(call1)
		// Second call is a duplicate — same From/To/Function — skipped.
		v.handleCall(call2)

		if len(v.frames.frames) != 1 {
			t.Errorf("expected 1 frame (duplicate skipped), got %d", len(v.frames.frames))
		}
	})
}

// TestGetTypePkgPath exercises every branch of defaultSequenceAnalyzer.getTypePkgPath.
func TestGetTypePkgPath(t *testing.T) {
	t.Parallel()

	// reusable named type for subtests 2-3
	pkg := types.NewPackage("example.com/pkg", "pkg")
	tn := types.NewTypeName(token.NoPos, pkg, "T", types.Typ[types.Int])
	named := types.NewNamed(tn, types.Typ[types.Int], nil)
	ptr := types.NewPointer(named)

	tests := []struct {
		name  string
		input types.Type
		want  string
	}{
		{
			name:  "nil type returns empty",
			input: nil,
			want:  "",
		},
		{
			name:  "pointer unwraps to named",
			input: ptr,
			want:  "example.com/pkg",
		},
		{
			name:  "named type returns package path",
			input: named,
			want:  "example.com/pkg",
		},
		{
			name:  "non-named non-pointer returns empty",
			input: types.NewSlice(types.Typ[types.Int]),
			want:  "",
		},
		{
			name: "named type with nil package",
			input: types.NewNamed(
				types.NewTypeName(token.NoPos, nil, "T", types.Typ[types.Int]),
				types.Typ[types.Int],
				nil,
			),
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := &defaultSequenceAnalyzer{}
			got := a.getTypePkgPath(tt.input)
			if got != tt.want {
				t.Errorf("getTypePkgPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestGetTypeName exercises every branch of defaultSequenceAnalyzer.getTypeName.
func TestGetTypeName(t *testing.T) {
	t.Parallel()

	pkg := types.NewPackage("example.com/pkg", "pkg")
	tn := types.NewTypeName(token.NoPos, pkg, "T", types.Typ[types.Int])
	named := types.NewNamed(tn, types.Typ[types.Int], nil)
	ptr := types.NewPointer(named)

	tests := []struct {
		name  string
		input types.Type
		want  string
	}{
		{
			name:  "nil type returns Unknown",
			input: nil,
			want:  "Unknown",
		},
		{
			name:  "pointer unwraps to named",
			input: ptr,
			want:  "T",
		},
		{
			name:  "named type returns name",
			input: named,
			want:  "T",
		},
		{
			name:  "non-named non-pointer returns string representation",
			input: types.NewSlice(types.Typ[types.Int]),
			want:  "[]int",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := &defaultSequenceAnalyzer{}
			got := a.getTypeName(tt.input)
			if got != tt.want {
				t.Errorf("getTypeName() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestResolveSelectorCall_PkgNameResolution exercises the PkgName branch of
// sequenceVisitor.resolveSelectorCall where sel.X is an *ast.Ident that
// resolves to a *types.PkgName (an import).
func TestResolveSelectorCall_PkgNameResolution(t *testing.T) {
	t.Parallel()
	_, f := parseTestFile(t, `package p
import "fmt"
func f() { fmt.Println() }`)
	fd := f.Decls[1].(*ast.FuncDecl)
	exprStmt := fd.Body.List[0].(*ast.ExprStmt)
	call := exprStmt.X.(*ast.CallExpr)
	sel := call.Fun.(*ast.SelectorExpr)

	fmtPkg := types.NewPackage("fmt", "fmt")
	pkgTypes := types.NewPackage("example.com/local/pkg", "p")

	pkgName := types.NewPkgName(token.NoPos, pkgTypes, "fmt", fmtPkg)

	printlnSig := types.NewSignatureType(nil, nil, nil, nil, nil, false)
	printlnFunc := types.NewFunc(token.NoPos, fmtPkg, "Println", printlnSig)

	uses := make(map[*ast.Ident]types.Object)
	uses[sel.X.(*ast.Ident)] = pkgName
	uses[sel.Sel] = printlnFunc

	v := &sequenceVisitor{
		ctx: context.Background(),
		pkg: &packages.Package{
			PkgPath:   "example.com/local/pkg",
			Types:     pkgTypes,
			TypesInfo: &types.Info{Uses: uses},
		},
		analyzer: &defaultSequenceAnalyzer{},
	}

	gotFunc, gotPkgPath, gotId := v.resolveSelectorCall(sel)
	if gotFunc != "Println" {
		t.Errorf("func = %q, want %q", gotFunc, "Println")
	}
	if gotPkgPath != "fmt" {
		t.Errorf("pkgPath = %q, want %q", gotPkgPath, "fmt")
	}
	if gotId != "fmt.Println" {
		t.Errorf("id = %q, want %q", gotId, "fmt.Println")
	}
}

// TestResolveSelectorCall_NonPkgNameObject exercises the non-PkgName branch
// where sel.X is an *ast.Ident that resolves to a non-PkgName object (e.g.
// a *types.Var), so the code falls into the else branch to derive the
// package path from the variable's type.
func TestResolveSelectorCall_NonPkgNameObject(t *testing.T) {
	t.Parallel()
	_, f := parseTestFile(t, `package p
type S struct{}
func (s S) Method() {}
func f() { var s S; s.Method() }`)
	fd := f.Decls[2].(*ast.FuncDecl)
	exprStmt := fd.Body.List[1].(*ast.ExprStmt)
	call := exprStmt.X.(*ast.CallExpr)
	sel := call.Fun.(*ast.SelectorExpr)

	pkgTypes := types.NewPackage("example.com/pkg", "p")
	namedS := types.NewNamed(
		types.NewTypeName(token.NoPos, pkgTypes, "S", types.NewStruct(nil, nil)),
		types.NewStruct(nil, nil),
		nil,
	)
	methodFunc := types.NewFunc(
		token.NoPos,
		pkgTypes,
		"Method",
		types.NewSignatureType(
			types.NewVar(token.NoPos, nil, "s", namedS),
			nil, nil, nil, nil, false,
		),
	)
	varS := types.NewVar(token.NoPos, pkgTypes, "s", namedS)

	uses := make(map[*ast.Ident]types.Object)
	uses[sel.X.(*ast.Ident)] = varS
	uses[sel.Sel] = methodFunc

	v := &sequenceVisitor{
		ctx: context.Background(),
		pkg: &packages.Package{
			PkgPath:   "example.com/local/pkg",
			TypesInfo: &types.Info{Uses: uses},
		},
		analyzer: &defaultSequenceAnalyzer{},
	}

	gotFunc, gotPkgPath, gotId := v.resolveSelectorCall(sel)
	if gotFunc != "Method" {
		t.Errorf("func = %q, want %q", gotFunc, "Method")
	}
	if gotPkgPath != "example.com/pkg" {
		t.Errorf("pkgPath = %q, want %q", gotPkgPath, "example.com/pkg")
	}
	if gotId != "example.com/pkg.S.Method" {
		t.Errorf("id = %q, want %q", gotId, "example.com/pkg.S.Method")
	}
}

// TestResolveSelectorCall_NonIdentX exercises the branch where sel.X is
// not an *ast.Ident (e.g. a composite literal like T{}), forcing the
// resolver to fall back to TypesInfo.Types to obtain the receiver type.
func TestResolveSelectorCall_NonIdentX(t *testing.T) {
	t.Parallel()
	_, f := parseTestFile(t, `package p
type T struct{}
func (t T) Method() {}
func f() { (T{}).Method() }`)
	fd := f.Decls[2].(*ast.FuncDecl)
	exprStmt := fd.Body.List[0].(*ast.ExprStmt)
	call := exprStmt.X.(*ast.CallExpr)
	sel := call.Fun.(*ast.SelectorExpr)

	pkgTypes := types.NewPackage("example.com/pkg", "p")
	namedT := types.NewNamed(
		types.NewTypeName(token.NoPos, pkgTypes, "T", types.NewStruct(nil, nil)),
		types.NewStruct(nil, nil),
		nil,
	)
	methodFunc := types.NewFunc(
		token.NoPos,
		pkgTypes,
		"Method",
		types.NewSignatureType(
			types.NewVar(token.NoPos, nil, "t", namedT),
			nil, nil, nil, nil, false,
		),
	)

	uses := make(map[*ast.Ident]types.Object)
	uses[sel.Sel] = methodFunc

	typesMap := make(map[ast.Expr]types.TypeAndValue)
	typesMap[sel.X] = types.TypeAndValue{Type: namedT}

	v := &sequenceVisitor{
		ctx: context.Background(),
		pkg: &packages.Package{
			PkgPath: "example.com/local/pkg",
			TypesInfo: &types.Info{
				Uses:  uses,
				Types: typesMap,
			},
		},
		analyzer: &defaultSequenceAnalyzer{},
	}

	gotFunc, gotPkgPath, gotId := v.resolveSelectorCall(sel)
	if gotFunc != "Method" {
		t.Errorf("func = %q, want %q", gotFunc, "Method")
	}
	if gotPkgPath != "example.com/pkg" {
		t.Errorf("pkgPath = %q, want %q", gotPkgPath, "example.com/pkg")
	}
	if gotId != "example.com/pkg.T.Method" {
		t.Errorf("id = %q, want %q", gotId, "example.com/pkg.T.Method")
	}
}

// TestResolveSelectorCall_BothPathsFail exercises the fallback path where
// neither the Ident-based nor the TypesInfo.Types-based resolution succeeds
// and the function returns only the selector name with empty pkgPath/id.
func TestResolveSelectorCall_BothPathsFail(t *testing.T) {
	t.Parallel()
	sel := &ast.SelectorExpr{
		X:   &ast.BasicLit{Kind: token.INT, Value: "1"},
		Sel: &ast.Ident{Name: "Foo"},
	}

	v := &sequenceVisitor{
		ctx: context.Background(),
		pkg: &packages.Package{
			PkgPath: "example.com/local/pkg",
			TypesInfo: &types.Info{
				Uses:  make(map[*ast.Ident]types.Object),
				Types: make(map[ast.Expr]types.TypeAndValue),
			},
		},
		analyzer: &defaultSequenceAnalyzer{},
	}

	gotFunc, gotPkgPath, gotId := v.resolveSelectorCall(sel)
	if gotFunc != "Foo" {
		t.Errorf("func = %q, want %q", gotFunc, "Foo")
	}
	if gotPkgPath != "" {
		t.Errorf("pkgPath = %q, want %q", gotPkgPath, "")
	}
	if gotId != "" {
		t.Errorf("id = %q, want %q", gotId, "")
	}
}

// TestResolveCallDetails exercises every branch of sequenceVisitor.resolveCallDetails.
func TestResolveCallDetails(t *testing.T) {
	t.Parallel()

	t.Run("non-interface call returns bare func name", func(t *testing.T) {
		t.Parallel()
		_, f := parseTestFile(t, `package p
func g() int { return 0 }
func f() { g() }`)
		fd := f.Decls[1].(*ast.FuncDecl)
		exprStmt := fd.Body.List[0].(*ast.ExprStmt)
		call := exprStmt.X.(*ast.CallExpr)

		typesMap := make(map[ast.Expr]types.TypeAndValue)
		typesMap[call] = types.TypeAndValue{Type: types.Typ[types.Int]}

		v := &sequenceVisitor{
			ctx: context.Background(),
			pkg: &packages.Package{
				TypesInfo: &types.Info{Types: typesMap},
			},
			analyzer: &defaultSequenceAnalyzer{},
		}

		displayFunc, retType := v.resolveCallDetails(call, "g")
		if displayFunc != "g" {
			t.Errorf("displayFunc = %q, want %q", displayFunc, "g")
		}
		if retType != "int" {
			t.Errorf("retType = %q, want %q", retType, "int")
		}
	})

	t.Run("interface call with type name", func(t *testing.T) {
		t.Parallel()
		_, f := parseTestFile(t, `package p
type Runner interface { Run() int }
func f(r Runner) { r.Run() }`)
		fd := f.Decls[1].(*ast.FuncDecl)
		exprStmt := fd.Body.List[0].(*ast.ExprStmt)
		call := exprStmt.X.(*ast.CallExpr)
		sel := call.Fun.(*ast.SelectorExpr) // r.Run

		pkgTypes := types.NewPackage("example.com/pkg", "p")

		// Build interface type
		runMethod := types.NewFunc(
			token.NoPos,
			pkgTypes,
			"Run",
			types.NewSignatureType(nil, nil, nil, nil,
				types.NewTuple(types.NewVar(token.NoPos, nil, "", types.Typ[types.Int])),
				false,
			),
		)
		iface := types.NewInterfaceType([]*types.Func{runMethod}, nil)
		namedIface := types.NewNamed(
			types.NewTypeName(token.NoPos, pkgTypes, "Runner", iface),
			iface,
			nil,
		)

		typesMap := make(map[ast.Expr]types.TypeAndValue)
		// sel.X is *ast.Ident "r" with interface type
		typesMap[sel.X] = types.TypeAndValue{Type: namedIface}
		// The call return type
		typesMap[call] = types.TypeAndValue{Type: types.Typ[types.Int]}

		v := &sequenceVisitor{
			ctx: context.Background(),
			pkg: &packages.Package{
				TypesInfo: &types.Info{Types: typesMap},
			},
			analyzer: &defaultSequenceAnalyzer{},
		}

		displayFunc, retType := v.resolveCallDetails(call, "Run")
		if displayFunc != "Runner.Run" {
			t.Errorf("displayFunc = %q, want %q", displayFunc, "Runner.Run")
		}
		if retType != "int" {
			t.Errorf("retType = %q, want %q", retType, "int")
		}
	})

	t.Run("void return cleared", func(t *testing.T) {
		t.Parallel()
		_, f := parseTestFile(t, "package p; func g() {}; func f() { g() }")
		fd := f.Decls[1].(*ast.FuncDecl)
		exprStmt := fd.Body.List[0].(*ast.ExprStmt)
		call := exprStmt.X.(*ast.CallExpr)

		typesMap := make(map[ast.Expr]types.TypeAndValue)
		// Zero-length tuple → String() == "()"
		typesMap[call] = types.TypeAndValue{Type: types.NewTuple()}

		v := &sequenceVisitor{
			ctx: context.Background(),
			pkg: &packages.Package{
				TypesInfo: &types.Info{Types: typesMap},
			},
			analyzer: &defaultSequenceAnalyzer{},
		}

		_, retType := v.resolveCallDetails(call, "g")
		if retType != "" {
			t.Errorf("retType = %q, want %q", retType, "")
		}
	})

	t.Run("invalid type return cleared", func(t *testing.T) {
		t.Parallel()
		_, f := parseTestFile(t, "package p; func g() {}; func f() { g() }")
		fd := f.Decls[1].(*ast.FuncDecl)
		exprStmt := fd.Body.List[0].(*ast.ExprStmt)
		call := exprStmt.X.(*ast.CallExpr)

		typesMap := make(map[ast.Expr]types.TypeAndValue)
		typesMap[call] = types.TypeAndValue{Type: types.Typ[types.Invalid]}

		v := &sequenceVisitor{
			ctx: context.Background(),
			pkg: &packages.Package{
				TypesInfo: &types.Info{Types: typesMap},
			},
			analyzer: &defaultSequenceAnalyzer{},
		}

		_, retType := v.resolveCallDetails(call, "g")
		if retType != "" {
			t.Errorf("retType = %q, want %q", retType, "")
		}
	})
}

// TestSequenceExprToString_Default exercises the default branch of the
// defaultSequenceAnalyzer.exprToString method (not the astutil.go standalone).
func TestSequenceExprToString_Default(t *testing.T) {
	t.Parallel()
	a := &defaultSequenceAnalyzer{}

	tests := []struct {
		name string
		expr ast.Expr
		want string
	}{
		{"default case returns empty", &ast.BasicLit{Kind: token.INT, Value: "42"}, ""},
		{"array type returns empty", &ast.ArrayType{Elt: &ast.Ident{Name: "byte"}}, ""},
		{"map type returns empty", &ast.MapType{Key: &ast.Ident{Name: "string"}, Value: &ast.Ident{Name: "int"}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := a.exprToString(tt.expr)
			if got != tt.want {
				t.Errorf("exprToString(%T) = %q, want %q", tt.expr, got, tt.want)
			}
		})
	}
}

// TestResolveCallDetails_detectInterface exercises every branch of
// sequenceVisitor.resolveCallDetails_detectInterface.
func TestResolveCallDetails_detectInterface(t *testing.T) {
	t.Parallel()

	t.Run("non-selector call returns false", func(t *testing.T) {
		t.Parallel()
		_, f := parseTestFile(t, "package p; func f() { g() }")
		fd := f.Decls[0].(*ast.FuncDecl)
		call := fd.Body.List[0].(*ast.ExprStmt).X.(*ast.CallExpr)

		v := &sequenceVisitor{
			ctx: context.Background(),
			pkg: &packages.Package{
				TypesInfo: &types.Info{Types: make(map[ast.Expr]types.TypeAndValue)},
			},
			analyzer: &defaultSequenceAnalyzer{},
		}

		isIface, name := v.resolveCallDetails_detectInterface(call)
		if isIface {
			t.Error("expected isInterface=false for bare function call")
		}
		if name != "" {
			t.Errorf("expected empty name, got %q", name)
		}
	})

	t.Run("selector with missing type info returns false", func(t *testing.T) {
		t.Parallel()
		_, f := parseTestFile(t, "package p; func f() { x.Y() }")
		fd := f.Decls[0].(*ast.FuncDecl)
		call := fd.Body.List[0].(*ast.ExprStmt).X.(*ast.CallExpr)

		v := &sequenceVisitor{
			ctx: context.Background(),
			pkg: &packages.Package{
				TypesInfo: &types.Info{Types: make(map[ast.Expr]types.TypeAndValue)},
			},
			analyzer: &defaultSequenceAnalyzer{},
		}

		isIface, name := v.resolveCallDetails_detectInterface(call)
		if isIface {
			t.Error("expected false when type info is missing")
		}
		if name != "" {
			t.Errorf("expected empty name, got %q", name)
		}
	})

	t.Run("concrete type returns false", func(t *testing.T) {
		t.Parallel()
		call := &ast.CallExpr{
			Fun: &ast.SelectorExpr{
				X:   &ast.Ident{Name: "s"},
				Sel: &ast.Ident{Name: "Method"},
			},
		}
		pkgTypes := types.NewPackage("example.com/pkg", "p")
		named := types.NewNamed(
			types.NewTypeName(token.NoPos, pkgTypes, "S", types.NewStruct(nil, nil)),
			types.NewStruct(nil, nil),
			nil,
		)
		typesMap := make(map[ast.Expr]types.TypeAndValue)
		typesMap[call.Fun.(*ast.SelectorExpr).X] = types.TypeAndValue{Type: named}

		v := &sequenceVisitor{
			ctx: context.Background(),
			pkg: &packages.Package{
				TypesInfo: &types.Info{Types: typesMap},
			},
			analyzer: &defaultSequenceAnalyzer{},
		}

		isIface, name := v.resolveCallDetails_detectInterface(call)
		if isIface {
			t.Error("expected false for concrete struct type")
		}
		if name != "" {
			t.Errorf("expected empty name, got %q", name)
		}
	})

	t.Run("named interface returns true with type name", func(t *testing.T) {
		t.Parallel()
		call := &ast.CallExpr{
			Fun: &ast.SelectorExpr{
				X:   &ast.Ident{Name: "r"},
				Sel: &ast.Ident{Name: "Run"},
			},
		}
		pkgTypes := types.NewPackage("example.com/pkg", "p")
		runMethod := types.NewFunc(token.NoPos, pkgTypes, "Run",
			types.NewSignatureType(nil, nil, nil, nil, nil, false))
		iface := types.NewInterfaceType([]*types.Func{runMethod}, nil)
		namedIface := types.NewNamed(
			types.NewTypeName(token.NoPos, pkgTypes, "Runner", iface),
			iface, nil,
		)
		typesMap := make(map[ast.Expr]types.TypeAndValue)
		typesMap[call.Fun.(*ast.SelectorExpr).X] = types.TypeAndValue{Type: namedIface}

		v := &sequenceVisitor{
			ctx: context.Background(),
			pkg: &packages.Package{
				TypesInfo: &types.Info{Types: typesMap},
			},
			analyzer: &defaultSequenceAnalyzer{},
		}

		isIface, name := v.resolveCallDetails_detectInterface(call)
		if !isIface {
			t.Error("expected true for named interface")
		}
		if name != "Runner" {
			t.Errorf("expected 'Runner', got %q", name)
		}
	})
}

// TestResolveCallDetails_formatDisplay exercises every branch of
// sequenceVisitor.resolveCallDetails_formatDisplay.
func TestResolveCallDetails_formatDisplay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		targetFunc        string
		isInterface       bool
		interfaceTypeName string
		want              string
	}{
		{
			name:       "concrete call returns bare name",
			targetFunc: "DoWork",
			want:       "DoWork",
		},
		{
			name:              "named interface prefixes type",
			targetFunc:        "Run",
			isInterface:       true,
			interfaceTypeName: "Runner",
			want:              "Runner.Run",
		},
		{
			name:        "anonymous interface falls back to Interface",
			targetFunc:  "Execute",
			isInterface: true,
			want:        "Interface.Execute",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := &sequenceVisitor{}
			got := v.resolveCallDetails_formatDisplay(tt.targetFunc, tt.isInterface, tt.interfaceTypeName)
			if got != tt.want {
				t.Errorf("formatDisplay(%q, %v, %q) = %q, want %q",
					tt.targetFunc, tt.isInterface, tt.interfaceTypeName, got, tt.want)
			}
		})
	}
}

// TestResolveCallDetails_extractReturnType exercises every branch of
// sequenceVisitor.resolveCallDetails_extractReturnType.
func TestResolveCallDetails_extractReturnType(t *testing.T) {
	t.Parallel()

	t.Run("missing type info returns empty", func(t *testing.T) {
		t.Parallel()
		call := &ast.CallExpr{Fun: &ast.Ident{Name: "f"}}
		v := &sequenceVisitor{
			ctx: context.Background(),
			pkg: &packages.Package{
				TypesInfo: &types.Info{Types: make(map[ast.Expr]types.TypeAndValue)},
			},
			analyzer: &defaultSequenceAnalyzer{},
		}
		if got := v.resolveCallDetails_extractReturnType(call); got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("void return cleared", func(t *testing.T) {
		t.Parallel()
		call := &ast.CallExpr{Fun: &ast.Ident{Name: "f"}}
		typesMap := make(map[ast.Expr]types.TypeAndValue)
		typesMap[call] = types.TypeAndValue{Type: types.NewTuple()}
		v := &sequenceVisitor{
			ctx: context.Background(),
			pkg: &packages.Package{
				TypesInfo: &types.Info{Types: typesMap},
			},
			analyzer: &defaultSequenceAnalyzer{},
		}
		if got := v.resolveCallDetails_extractReturnType(call); got != "" {
			t.Errorf("expected empty for void, got %q", got)
		}
	})

	t.Run("invalid type cleared", func(t *testing.T) {
		t.Parallel()
		call := &ast.CallExpr{Fun: &ast.Ident{Name: "f"}}
		typesMap := make(map[ast.Expr]types.TypeAndValue)
		typesMap[call] = types.TypeAndValue{Type: types.Typ[types.Invalid]}
		v := &sequenceVisitor{
			ctx: context.Background(),
			pkg: &packages.Package{
				TypesInfo: &types.Info{Types: typesMap},
			},
			analyzer: &defaultSequenceAnalyzer{},
		}
		if got := v.resolveCallDetails_extractReturnType(call); got != "" {
			t.Errorf("expected empty for invalid, got %q", got)
		}
	})

	t.Run("valid named type returned", func(t *testing.T) {
		t.Parallel()
		call := &ast.CallExpr{Fun: &ast.Ident{Name: "f"}}
		pkgTypes := types.NewPackage("example.com/pkg", "p")
		named := types.NewNamed(
			types.NewTypeName(token.NoPos, pkgTypes, "Response", types.NewStruct(nil, nil)),
			types.NewStruct(nil, nil),
			nil,
		)
		typesMap := make(map[ast.Expr]types.TypeAndValue)
		typesMap[call] = types.TypeAndValue{Type: named}
		v := &sequenceVisitor{
			ctx: context.Background(),
			pkg: &packages.Package{
				TypesInfo: &types.Info{Types: typesMap},
			},
			analyzer: &defaultSequenceAnalyzer{},
		}
		if got := v.resolveCallDetails_extractReturnType(call); got != "Response" {
			t.Errorf("expected 'Response', got %q", got)
		}
	})
}

// TestSequenceVisitor_TryRecurse_NilIndexer verifies that tryRecurse
// does not panic when the analyzer's idx field is nil. Future refactors
// that add callers to tryRecurse without the upstream nil-guard in
// traceFlow must still be safe.
func TestSequenceVisitor_TryRecurse_NilIndexer(t *testing.T) {
	t.Parallel()
	a := &defaultSequenceAnalyzer{
		idx:     nil, // deliberately nil
		funcMap: make(map[string]funcInfo),
		pkgMu:   sync.RWMutex{},
	}
	v := &sequenceVisitor{
		ctx:      context.Background(),
		analyzer: a,
		frames:   &frameCollector{},
		visited:  make(map[string]bool),
		depth:    0,
		maxDepth: 5,
	}

	// This should not panic.
	v.tryRecurse(context.Background(), "some.package.Func", 0, 5, nil)

	// No assertion needed beyond "didn't panic".
}

// TestSequenceVisitor_TryRecurse_WithIndexer verifies that tryRecurse
// correctly recurses when the indexer is non-nil and the target is
// found in funcMap.
func TestSequenceVisitor_TryRecurse_WithIndexer(t *testing.T) {
	t.Parallel()
	pkgA, _ := setupMockPackages()
	idx := &mockIndexer{pkgs: []*packages.Package{pkgA}}

	a := newSequenceAnalyzer(&mockExecutor{}, &mockSecurityProvider{}, idx)
	a.pkgMu.Lock()
	a.pkgs = idx.pkgs
	a.funcMap = a.mapSymbols(idx.pkgs)
	a.pkgMu.Unlock()

	frames := &frameCollector{}
	visited := make(map[string]bool)
	v := &sequenceVisitor{
		ctx:      context.Background(),
		pkg:      pkgA,
		analyzer: a,
		frames:   frames,
		visited:  visited,
		depth:    0,
		maxDepth: 5,
		modName:  "github.com/test/mod",
	}

	// StartFunc is in funcMap — tryRecurse should add frames via walk
	startId := pkgA.PkgPath + ".StartFunc"
	v.tryRecurse(context.Background(), startId, 0, 5, nil)

	if len(frames.frames) == 0 {
		t.Error("expected frames to be added when target is in funcMap, got none")
	}
}
