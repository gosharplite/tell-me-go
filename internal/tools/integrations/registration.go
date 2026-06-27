// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"

	// Side-effect imports: each package's init() calls plugin.Register()
	// to self-register its integration tools. Registration order is
	// determined by Go's init() execution order (imports first, then
	// same-package in filename lexical order).
	_ "github.com/gosharplite/tell-me-go/internal/tools/integrations/ado"
	_ "github.com/gosharplite/tell-me-go/internal/tools/integrations/atlassian"

	"github.com/gosharplite/tell-me-go/internal/tools/integrations/plugin"
)

// RegisterAll registers all external integration tools by iterating over
// auto-registered plugins. Adding a new integration requires only:
// 1. Creating a sub-package with a plugin.Plugin implementation
// 2. Adding a blank import above (for the init() side effect)
//
// No other changes to this file are needed.
func RegisterAll(r tools.Registry, fs persistence.FileSystem, sm domain_security.Manager, client llm.LLMClient, assetsDir string) error {
	deps := plugin.PluginDependencies{
		FileSystem:  fs,
		SecurityMgr: sm,
		LLMClient:   client,
		AssetsDir:   assetsDir,
	}

	for _, p := range plugin.All() {
		if err := p.Register(r, deps); err != nil {
			return fmt.Errorf("%s: %w", p.Name(), err)
		}
	}

	return nil
}
