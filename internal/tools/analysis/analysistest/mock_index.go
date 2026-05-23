// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysistest

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/tools/analysis"
	"golang.org/x/tools/go/packages"
)

// MockSymbolIndex is a configurable mock of analysis.SymbolIndex for use in tests.
// Set Func fields to control behavior; zero-value fields provide safe defaults.
type MockSymbolIndex struct {
	GetImplementationsFunc func(ctx context.Context, id string) []string
	PackagesFunc           func(ctx context.Context, hb chan<- struct{}) ([]*packages.Package, error)
	GetUsagesFunc          func(ctx context.Context, symbol string, path string, hb chan<- struct{}) ([]analysis.Location, error)
	IsSymbolUsedFunc       func(ctx context.Context, name string, hb chan<- struct{}) bool
}

func (m *MockSymbolIndex) Lookup(ctx context.Context, symbol string, hb chan<- struct{}) ([]analysis.Location, error) {
	return nil, nil
}
func (m *MockSymbolIndex) FindImplementors(ctx context.Context, interfaceName string, hb chan<- struct{}) ([]analysis.TypeName, error) {
	return nil, nil
}
func (m *MockSymbolIndex) SearchSymbols(ctx context.Context, path string, query string, exportedOnly bool, hb chan<- struct{}) ([]analysis.SymbolLocation, error) {
	return nil, nil
}
func (m *MockSymbolIndex) GetUsages(ctx context.Context, symbol string, path string, hb chan<- struct{}) ([]analysis.Location, error) {
	if m.GetUsagesFunc != nil {
		return m.GetUsagesFunc(ctx, symbol, path, hb)
	}
	return nil, nil
}
func (m *MockSymbolIndex) IsSymbolUsed(ctx context.Context, name string, hb chan<- struct{}) bool {
	if m.IsSymbolUsedFunc != nil {
		return m.IsSymbolUsedFunc(ctx, name, hb)
	}
	return false
}
func (m *MockSymbolIndex) GetImplementations(ctx context.Context, interfaceMethodId string, hb chan<- struct{}) []string {
	if m.GetImplementationsFunc != nil {
		return m.GetImplementationsFunc(ctx, interfaceMethodId)
	}
	return nil
}
func (m *MockSymbolIndex) Packages(ctx context.Context, hb chan<- struct{}) ([]*packages.Package, error) {
	if m.PackagesFunc != nil {
		return m.PackagesFunc(ctx, hb)
	}
	return nil, nil
}
func (m *MockSymbolIndex) Refresh(ctx context.Context, hb chan<- struct{}) error { return nil }

func (m *MockSymbolIndex) WarmImplementations(ctx context.Context) {}
