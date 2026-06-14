// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"fmt"
	"go/token"
	"go/types"
	"testing"
	"time"
)

// newTestFuncObj creates a minimal types.Object (a *types.Func) for testing.
// The returned object has token.NoPos, which causes isNolintDeadcode to
// return false and both complexity/impact calculations to return 0.
func newTestFuncObj(pkgPath, name string) types.Object {
	pkg := types.NewPackage(pkgPath, name)
	sig := types.NewSignatureType(nil, nil, nil, nil, nil, false)
	return types.NewFunc(token.NoPos, pkg, name, sig)
}

func TestCollectOrphanFindings_Heartbeat(t *testing.T) {
	t.Parallel()
	state := &scanState{
		pkgs:         nil, // hasTextMatchOutsidePackage safely iterates nil
		declarations: make(map[string]*symMeta),
		totalUses:    make(map[string]int),
		externalUses: make(map[string]int),
	}
	// Create 21 declarations with real types.Object to trigger
	// i%20==0 at i=0 and i=20 AND cover the findings-append path.
	for i := 0; i < 21; i++ {
		id := fmt.Sprintf("example.com/pkg.Symbol%d", i)
		state.declarations[id] = &symMeta{
			id:      id,
			pkgPath: "example.com/pkg",
			name:    fmt.Sprintf("Symbol%d", i),
			symType: "Function",
			obj:     newTestFuncObj("example.com/pkg", fmt.Sprintf("Symbol%d", i)),
		}
		// totalUses[id] == 0 → severityClassifer assigns DEAD → report is non-nil
	}
	a := &defaultDeadCodeAnalyzer{}
	hb := make(chan struct{}, 2)

	findings := a.collectOrphanFindings(context.Background(), state, false, hb)

	select {
	case <-hb:
	case <-time.After(time.Second):
		t.Error("expected heartbeat at i=0")
	}
	if len(findings) != 21 {
		t.Errorf("expected 21 findings, got %d", len(findings))
	}
}

func TestCollectOrphanFindings_NilHeartbeat(t *testing.T) {
	t.Parallel()
	state := &scanState{
		declarations: map[string]*symMeta{
			"example.com/pkg.Func": {
				id: "example.com/pkg.Func", pkgPath: "example.com/pkg",
				name: "Func", symType: "Function", obj: nil,
			},
		},
		totalUses:    make(map[string]int),
		externalUses: make(map[string]int),
	}
	a := &defaultDeadCodeAnalyzer{}
	findings := a.collectOrphanFindings(context.Background(), state, false, nil)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for nil-obj declaration, got %d", len(findings))
	}
}

func TestCollectOrphanFindings_FullBuffer(t *testing.T) {
	t.Parallel()
	state := &scanState{
		pkgs:         nil,
		declarations: make(map[string]*symMeta),
		totalUses:    make(map[string]int),
		externalUses: make(map[string]int),
	}
	// One declaration is enough — i=0 triggers the heartbeat.
	state.declarations["example.com/pkg.Func"] = &symMeta{
		id:      "example.com/pkg.Func",
		pkgPath: "example.com/pkg",
		name:    "Func",
		symType: "Function",
		obj:     newTestFuncObj("example.com/pkg", "Func"),
	}
	a := &defaultDeadCodeAnalyzer{}
	// Pre-fill the buffer so the select falls through to default:
	hb := make(chan struct{}, 1)
	hb <- struct{}{}

	findings := a.collectOrphanFindings(context.Background(), state, false, hb)
	if len(findings) != 1 {
		t.Errorf("expected 1 finding, got %d", len(findings))
	}
}
