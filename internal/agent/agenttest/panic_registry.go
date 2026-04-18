// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// PanicRegistry is a test double for tools.Registry that can be
// configured to panic from GetDeclarations or Execute. Use it to
// verify that callers recover from registry-side panics correctly.
// The embedded tools.Registry satisfies the interface for any method
// not explicitly overridden here; tests should not rely on those
// inherited methods doing anything useful.
type PanicRegistry struct {
	tools.Registry
	PanicOnExec bool
	PanicOnGet  bool
	Serial      bool
}

func (r *PanicRegistry) GetDeclarations() []*tools.ToolDeclaration {
	if r.PanicOnGet {
		panic("registry GetDeclarations panic")
	}
	return []*tools.ToolDeclaration{{Name: "any"}}
}

func (r *PanicRegistry) IsSerial(name string) bool {
	return r.Serial
}

func (r *PanicRegistry) IsLongRunning(name string) bool {
	return false
}

func (r *PanicRegistry) Execute(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	if r.PanicOnExec {
		panic("registry Execute panic")
	}
	return tools.ToolResult{}, nil
}

func (r *PanicRegistry) GetOptions(name string) tools.ToolOptions {
	return tools.ToolOptions{Serial: r.IsSerial(name), LongRunning: r.IsLongRunning(name)}
}

func (r *PanicRegistry) RegisterToToolkit(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	return nil
}

func (r *PanicRegistry) RegisterToToolkitWithOptions(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return nil
}

func (r *PanicRegistry) GetCoreDeclarations() []*tools.ToolDeclaration {
	return r.GetDeclarations()
}

func (r *PanicRegistry) GetDeclarationsByToolkits(toolkits []string) []*tools.ToolDeclaration {
	return r.GetDeclarations()
}

func (r *PanicRegistry) ListAvailableToolkits() []string {
	return []string{"core"}
}
