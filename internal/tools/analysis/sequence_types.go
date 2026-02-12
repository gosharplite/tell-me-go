// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"

	"golang.org/x/tools/go/packages"
)

// callFrame represents a single step in a sequence diagram.
type callFrame struct {
	From     string
	To       string
	Function string
	Async    bool
	InLoop   bool
	Return   string
}

// iGopackageProvider defines the interface for loading Go packages.
type iGopackageProvider interface {
	LoadPackages(ctx context.Context, patterns ...string) ([]*packages.Package, error)
}

// realGopackageProvider implements iGopackageProvider using x/tools/go/packages.
type realGopackageProvider struct{}

func (p *realGopackageProvider) LoadPackages(ctx context.Context, patterns ...string) ([]*packages.Package, error) {
	cfg := &packages.Config{
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo,
		Context: ctx,
	}
	return packages.Load(cfg, patterns...)
}
