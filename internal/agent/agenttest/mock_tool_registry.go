// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// MockToolRegistry is a test double for tools.Registry. It records the
// declarations and handlers registered through it and supports scripted
// failures via RegisterErr/FailAfter, allowing tests to assert
// registration counting and bail-out semantics.
type MockToolRegistry struct {
	Declarations []*tools.ToolDeclaration
	ToolkitMap   map[string][]*tools.ToolDeclaration
	Handlers     map[string]tools.ToolFunc
	Options      map[string]tools.ToolOptions
	RegisterErr  error
	FailAfter    int
	CallCount    int
}

// NewMockToolRegistry returns a MockToolRegistry with all internal maps
// preallocated, so callers can register tools without needing to
// initialise the zero value first.
func NewMockToolRegistry() *MockToolRegistry {
	return &MockToolRegistry{
		ToolkitMap: make(map[string][]*tools.ToolDeclaration),
		Handlers:   make(map[string]tools.ToolFunc),
		Options:    make(map[string]tools.ToolOptions),
	}
}

func (m *MockToolRegistry) GetDeclarations() []*tools.ToolDeclaration {
	return m.Declarations
}

func (m *MockToolRegistry) Register(declaration *tools.ToolDeclaration, implementation tools.ToolFunc) error {
	return m.RegisterToToolkit("core", declaration, implementation)
}

func (m *MockToolRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return m.RegisterToToolkitWithOptions("core", def, handler, opts)
}

func (m *MockToolRegistry) Execute(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	if handler, ok := m.Handlers[name]; ok {
		return handler(ctx, args, hb)
	}
	return tools.ToolResult{}, nil
}

func (m *MockToolRegistry) IsSerial(name string) bool {
	return m.Options[name].Serial
}

func (m *MockToolRegistry) IsLongRunning(name string) bool {
	return m.Options[name].LongRunning
}

func (m *MockToolRegistry) GetOptions(name string) tools.ToolOptions {
	return m.Options[name]
}

func (m *MockToolRegistry) RegisterToToolkit(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	return m.RegisterToToolkitWithOptions(toolkit, def, handler, tools.ToolOptions{})
}

func (m *MockToolRegistry) RegisterToToolkitWithOptions(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	m.CallCount++
	if m.RegisterErr != nil && (m.FailAfter == 0 || m.CallCount > m.FailAfter) {
		return m.RegisterErr
	}
	m.Declarations = append(m.Declarations, def)
	if m.ToolkitMap == nil {
		m.ToolkitMap = make(map[string][]*tools.ToolDeclaration)
	}
	m.ToolkitMap[toolkit] = append(m.ToolkitMap[toolkit], def)
	if m.Handlers == nil {
		m.Handlers = make(map[string]tools.ToolFunc)
	}
	m.Handlers[def.Name] = handler
	if m.Options == nil {
		m.Options = make(map[string]tools.ToolOptions)
	}
	m.Options[def.Name] = opts
	return nil
}

func (m *MockToolRegistry) GetCoreDeclarations() []*tools.ToolDeclaration {
	return m.ToolkitMap["core"]
}

func (m *MockToolRegistry) GetDeclarationsByToolkits(toolkits []string) []*tools.ToolDeclaration {
	dedup := make(map[string]*tools.ToolDeclaration)
	for _, d := range m.ToolkitMap["core"] {
		dedup[d.Name] = d
	}
	for _, tk := range toolkits {
		for _, d := range m.ToolkitMap[tk] {
			dedup[d.Name] = d
		}
	}
	res := make([]*tools.ToolDeclaration, 0, len(dedup))
	for _, d := range dedup {
		res = append(res, d)
	}
	return res
}

func (m *MockToolRegistry) ListAvailableToolkits() []string {
	toolkits := make([]string, 0, len(m.ToolkitMap))
	for tk := range m.ToolkitMap {
		toolkits = append(toolkits, tk)
	}
	return toolkits
}

func (m *MockToolRegistry) SetRegisterErr(err error) { m.RegisterErr = err }
func (m *MockToolRegistry) SetFailAfter(n int)       { m.FailAfter = n }
