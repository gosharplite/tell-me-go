// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package atlassian

import (
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/integrations/plugin"
)

// atlassianPlugin implements plugin.Plugin for both Confluence and Jira toolkits.
// A single plugin registers both toolkits because they share the same Atlassian
// provider infrastructure.
type atlassianPlugin struct{}

// NewPlugin returns a new Atlassian plugin for the global registry.
func NewPlugin() plugin.Plugin { return &atlassianPlugin{} }

func (atlassianPlugin) Name() string { return "atlassian" }

func (atlassianPlugin) Register(r tools.Registry, deps plugin.PluginDependencies) error {
	if err := RegisterConfluence(r, deps.SecurityMgr, deps.HTTPClient); err != nil {
		return fmt.Errorf("confluence: %w", err)
	}
	if err := RegisterJira(r, deps.SecurityMgr, deps.HTTPClient); err != nil {
		return fmt.Errorf("jira: %w", err)
	}
	return nil
}

func init() {
	_ = plugin.Register(NewPlugin()) //nolint:errcheck // init() cannot return errors; duplicate names caught in tests
}
