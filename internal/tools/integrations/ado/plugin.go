// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/integrations/plugin"
)

// adoPlugin implements plugin.Plugin for the Azure DevOps toolkit.
type adoPlugin struct{}

// NewPlugin returns a new ADO plugin for the global registry.
func NewPlugin() plugin.Plugin { return &adoPlugin{} }

func (adoPlugin) Name() string { return "azure-devops" }

func (adoPlugin) Register(r tools.Registry, deps plugin.PluginDependencies) error {
	return Register(r, deps.SecurityMgr, deps.HTTPClient)
}

func init() {
	_ = plugin.Register(NewPlugin()) //nolint:errcheck // init() cannot return errors; duplicate names caught in tests
}
