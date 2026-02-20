// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"fmt"
)

type MockRegistry struct {
	Handlers     map[string]ToolFunc
	Declarations []*ToolDeclaration
	Serial       map[string]bool
	LongRunning  map[string]bool
}

func NewMockRegistry() *MockRegistry {
	return &MockRegistry{
		Handlers:     make(map[string]ToolFunc),
		Declarations: make([]*ToolDeclaration, 0),
		Serial:       make(map[string]bool),
		LongRunning:  make(map[string]bool),
	}
}

func (m *MockRegistry) Register(def *ToolDeclaration, handler ToolFunc) {
	m.RegisterWithOptions(def, handler, ToolOptions{})
}

func (m *MockRegistry) RegisterWithOptions(def *ToolDeclaration, handler ToolFunc, opts ToolOptions) {
	m.Declarations = append(m.Declarations, def)
	m.Handlers[def.Name] = handler
	if opts.Serial {
		m.Serial[def.Name] = true
	}
	if opts.LongRunning {
		m.LongRunning[def.Name] = true
	}
}

func (m *MockRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (ToolResult, error) {
	handler, ok := m.Handlers[name]
	if !ok {
		return ToolResult{}, fmt.Errorf("tool not found: %s", name)
	}
	return handler(ctx, args)
}

func (m *MockRegistry) IsSerial(name string) bool {
	return m.Serial[name]
}

func (m *MockRegistry) IsLongRunning(name string) bool {
	return m.LongRunning[name]
}

func (m *MockRegistry) GetDeclarations() []*ToolDeclaration {
	return m.Declarations
}
