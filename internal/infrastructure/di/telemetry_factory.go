package di

import (
	stdctx "context"
	"errors"
	"log/slog"
	"path/filepath"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/logging"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/telemetry"
)

type TelemetryFactory interface {
	BuildTelemetry(ctx stdctx.Context, paths *persistence.Paths, cfg *config.Config, cleanup func(stdctx.Context) error) (pricing.PricingData, pricing.CostTracker, ports.TurnsLogger, func(stdctx.Context) error)
}

type DefaultTelemetryFactory struct {
	HomeDir    string
	FileSystem infra_persistence.FileSystem
	SM         ConfigurableSecurityManager
	Logger     *slog.Logger
}

func NewTelemetryFactory(homeDir string, fs infra_persistence.FileSystem, sm ConfigurableSecurityManager, logger *slog.Logger) TelemetryFactory {
	if logger == nil {
		logger = slog.Default()
	}
	return &DefaultTelemetryFactory{
		HomeDir:    homeDir,
		FileSystem: fs,
		SM:         sm,
		Logger:     logger,
	}
}

func (f *DefaultTelemetryFactory) BuildTelemetry(ctx stdctx.Context, paths *persistence.Paths, cfg *config.Config, cleanup func(stdctx.Context) error) (pricing.PricingData, pricing.CostTracker, ports.TurnsLogger, func(stdctx.Context) error) {
	pricingData := telemetry.GetPricing(ctx, f.SM, filepath.Join(f.HomeDir, "output"))

	modelPricing := telemetry.GetModelPricing(cfg.Model, pricingData)
	tracker := telemetry.NewSessionCostTracker(f.SM, paths.LogPath, cfg.Mode, cfg.Model, modelPricing, pricingData)
	tracker.Warmup()

	var turnsLogger ports.TurnsLogger = &ports.NoOpTurnsLogger{}
	if paths.TurnsLogPath != "" {
		if tl, err := logging.NewAsyncTurnsLogger(f.FileSystem, paths.TurnsLogPath, f.Logger); err == nil {
			turnsLogger = tl

			origCleanup := cleanup
			cleanup = func(c stdctx.Context) error {
				var err error
				if origCleanup != nil {
					err = origCleanup(c)
				}
				return errors.Join(err, tl.Close())
			}
		} else {
			f.Logger.Warn("failed to initialize turns logger, falling back to no-op", "error", err, "path", paths.TurnsLogPath)
		}
	}

	return pricingData, tracker, turnsLogger, cleanup
}
