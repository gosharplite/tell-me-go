// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/integrations/ado"
	"github.com/gosharplite/tell-me-go/internal/tools/integrations/atlassian"
)

// RegisterAll registers all external integration tools.
func RegisterAll(r tools.Registry, fs persistence.FileSystem, sm domain_security.Manager, client llm.LLMClient, assetsDir string) error {
	// Register Media Tools
	if err := registerMedia(r, fs, sm, client, assetsDir); err != nil {
		return err
	}

	// Register Network Tools
	net := newnetworkTool(sm, nil)
	if err := registerNetwork(r, net); err != nil {
		return err
	}

	// Register Teams Tools
	if err := registerTeams(r, sm, nil); err != nil {
		return err
	}

	// Register Confluence Tools
	if err := atlassian.RegisterConfluence(r, sm, nil); err != nil {
		return err
	}

	// Register Jira Tools
	if err := atlassian.RegisterJira(r, sm, nil); err != nil {
		return err
	}

	// Register Azure DevOps Tools
	if err := ado.Register(r, sm, nil); err != nil {
		return err
	}

	return nil
}
