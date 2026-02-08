// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package chat

import (
	"path/filepath"

	"github.com/gosharplite/tell-me-go/internal/config"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/pricing"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/session"
	mediasvc "github.com/gosharplite/tell-me-go/internal/services/media"
	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

func (c *Command) setupRegistry(client *llm.Client, cfg *config.Config, paths *session.Paths, pricingOverrides map[string]pricing.ModelPricing) *registry.Registry {
	reg := registry.New()

	gateway := mediasvc.NewService(client, filepath.Join(c.HomeDir, "assets/generated"))

	tools.RegisterAll(
		reg,
		c.SM,
		paths.ModeDir,
		paths.LogPath,
		cfg.Model,
		cfg.Mode,
		pricingOverrides,
		gateway,
	)

	return reg
}
