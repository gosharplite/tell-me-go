// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	"context"
	"io"
	"log/slog"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	infra_llm "github.com/gosharplite/tell-me-go/internal/infrastructure/llm"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/telemetry"
	infra_tools "github.com/gosharplite/tell-me-go/internal/tools"
)

// BootstrapperConfig is a pure value object holding all configuration
// needed to create a Bootstrapper. It contains both direct values and
// factory functions that wire infrastructure components together.
type BootstrapperConfig struct {
	HomeDir         string
	SM              ConfigurableSecurityManager
	Version         string
	Stdout          io.Writer
	Stderr          io.Writer
	Logger          *slog.Logger
	FileSystem      infra_persistence.FileSystem
	WorkspacePolicy services.WorkspacePolicy

	// Factory functions
	ClientFactory    ports.ClientFactory
	RegisterAllTools func(infra_tools.ToolRegistrationParams) error
	RegisterMetrics  func(tools.Registry, security.Manager, string, string, string, string, map[string]pricing.ModelPricing, ports.KVStore) error
	RotateSession    func(context.Context, infra_persistence.FileSystem, io.Writer, persistence.Paths, int, *slog.Logger) error
	NewSessionState  func(context.Context, string, ...infra_persistence.SessionStateOption) (ports.SessionProvider, error)
}

// DefaultBootstrapperConfig returns a BootstrapperConfig populated with
// sensible production defaults. All direct-value fields are zero-valued;
// factory functions point to the canonical infrastructure implementations.
func DefaultBootstrapperConfig() BootstrapperConfig {
	return BootstrapperConfig{
		Logger:           slog.Default(),
		FileSystem:       &infra_persistence.OSFileSystem{},
		ClientFactory:    &infra_llm.DefaultClientFactory{},
		RegisterAllTools: infra_tools.RegisterAll,
		RegisterMetrics:  telemetry.RegisterMetrics,
		RotateSession:    infra_persistence.RotateSession,
		NewSessionState:  infra_persistence.NewSessionState,
	}
}
