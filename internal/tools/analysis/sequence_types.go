// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"

	"golang.org/x/tools/go/packages"
)

// CallFrame represents a single step in a sequence diagram.
type CallFrame struct {
	From     string
	To       string
	Function string
	Async    bool
	InLoop   bool
	Return   string
}

// IGoPackageProvider defines the interface for loading Go packages.
type IGoPackageProvider interface {
	LoadPackages(ctx context.Context, patterns ...string) ([]*packages.Package, error)
}

// RealGoPackageProvider implements IGoPackageProvider using x/tools/go/packages.
type RealGoPackageProvider struct{}

func (p *RealGoPackageProvider) LoadPackages(ctx context.Context, patterns ...string) ([]*packages.Package, error) {
	cfg := &packages.Config{
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo,
		Context: ctx,
	}
	return packages.Load(cfg, patterns...)
}
