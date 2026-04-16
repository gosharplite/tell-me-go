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

type telemetryFactory interface {
	BuildTelemetry(ctx stdctx.Context, paths *persistence.Paths, cfg *config.Config, pricingOverrides map[string]pricing.ModelPricing, cleanup func(stdctx.Context) error) (pricing.PricingData, pricing.CostTracker, ports.TurnsLogger, func(stdctx.Context) error)
}

type defaultTelemetryFactory struct {
	HomeDir    string
	FileSystem infra_persistence.FileSystem
	SM         ConfigurableSecurityManager
	Logger     *slog.Logger
}

func newTelemetryFactory(homeDir string, fs infra_persistence.FileSystem, sm ConfigurableSecurityManager, logger *slog.Logger) telemetryFactory {
	if logger == nil {
		logger = slog.Default()
	}
	return &defaultTelemetryFactory{
		HomeDir:    homeDir,
		FileSystem: fs,
		SM:         sm,
		Logger:     logger,
	}
}

func (f *defaultTelemetryFactory) BuildTelemetry(ctx stdctx.Context, paths *persistence.Paths, cfg *config.Config, pricingOverrides map[string]pricing.ModelPricing, cleanup func(stdctx.Context) error) (pricing.PricingData, pricing.CostTracker, ports.TurnsLogger, func(stdctx.Context) error) {
	f.Logger.Debug("Building telemetry",
		slog.String("model", cfg.Model),
		slog.Int("pricing_overrides_count", len(pricingOverrides)))
	
	pricingData := telemetry.GetPricing(ctx, f.SM, filepath.Join(f.HomeDir, "output"))
	f.Logger.Debug("Base pricing data loaded",
		slog.Int("total_models", len(pricingData.Models)))
	
	// Apply pricing overrides from config
	if len(pricingOverrides) > 0 {
		f.Logger.Debug("Applying pricing overrides")
		for modelName, override := range pricingOverrides {
			f.Logger.Debug("Pricing override",
				slog.String("model", modelName),
				slog.Float64("hit", override.Hit),
				slog.Float64("miss", override.Miss),
				slog.Float64("comp", override.Comp),
				slog.Float64("thinking", override.Thinking))
			pricingData.Models[modelName] = override
		}
		f.Logger.Debug("After applying overrides",
			slog.Int("total_models", len(pricingData.Models)))
	} else {
		f.Logger.Debug("No pricing overrides to apply")
	}
	
	modelPricing := telemetry.GetModelPricing(cfg.Model, pricingData)
	f.Logger.Debug("Retrieved model pricing",
		slog.String("model", cfg.Model),
		slog.Float64("hit", modelPricing.Hit),
		slog.Float64("miss", modelPricing.Miss),
		slog.Float64("comp", modelPricing.Comp),
		slog.Float64("thinking", modelPricing.Thinking))
	
	tracker := telemetry.NewSessionCostTracker(f.SM, paths.LogPath, cfg.Mode, cfg.Model, modelPricing, pricingData)
	tracker.Warmup()

	var turnsLogger ports.TurnsLogger = &ports.NoOpTurnsLogger{}
	if paths.TurnsLogPath != "" {
		if tl, err := logging.NewAsyncTurnsLogger(ctx, f.FileSystem, paths.TurnsLogPath, f.Logger); err == nil {
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
