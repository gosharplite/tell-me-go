// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"path/filepath"

	"github.com/gosharplite/tell-me-go/internal/config"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/pricing"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/session"
	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

func (c *ChatCommand) setupRegistry(client *llm.Client, cfg *config.Config, paths *session.Paths, pricingOverrides map[string]pricing.ModelPricing) *registry.Registry {
	reg := registry.New()

	tools.RegisterAll(
		reg,
		c.SM,
		paths.ModeDir,
		paths.LogPath,
		cfg.Model,
		cfg.Mode,
		pricingOverrides,
		client,
		filepath.Join(c.HomeDir, "assets/generated"),
	)

	return reg
}
