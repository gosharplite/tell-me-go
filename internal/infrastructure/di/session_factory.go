package di

import (
	stdctx "context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/telemetry"
)

type SessionFactory interface {
	BuildSession(ctx stdctx.Context, cfg *config.Config, configPath string, newSession bool, pricingOverrides map[string]pricing.ModelPricing) (ports.SessionProvider, *persistence.Paths, func(stdctx.Context) error, error)
}

type DefaultSessionFactory struct {
	HomeDir         string
	FileSystem      infra_persistence.FileSystem
	SM              ConfigurableSecurityManager
	Stderr          io.Writer
	Stdout          io.Writer
	Logger          *slog.Logger
	RotateSession   func(fs infra_persistence.FileSystem, stdout io.Writer, paths persistence.Paths, retentionDays int) error
	NewSessionState func(ctx stdctx.Context, modeDir string) (ports.SessionProvider, error)
}

func NewSessionFactory(homeDir string, fs infra_persistence.FileSystem, sm ConfigurableSecurityManager, stdout, stderr io.Writer, logger *slog.Logger, rotate func(fs infra_persistence.FileSystem, stdout io.Writer, paths persistence.Paths, retentionDays int) error, newState func(ctx stdctx.Context, modeDir string) (ports.SessionProvider, error)) SessionFactory {
	return &DefaultSessionFactory{
		HomeDir:         homeDir,
		FileSystem:      fs,
		SM:              sm,
		Stdout:          stdout,
		Stderr:          stderr,
		Logger:          logger,
		RotateSession:   rotate,
		NewSessionState: newState,
	}
}

func (f *DefaultSessionFactory) buildSessionProvider(ctx stdctx.Context, paths *persistence.Paths, cfg *config.Config) (ports.SessionProvider, func(stdctx.Context) error, error) {
	var sessionProvider ports.SessionProvider
	state, err := f.NewSessionState(ctx, paths.ModeDir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize session state: %w", err)
	}

	sessionProvider = state
	info := state.GetInfo()
	info.Model = cfg.Model
	info.Provider = cfg.SelectedProvider
	state.SetInfo(info)

	cleanup := func(stdctx.Context) error {
		if sessionProvider != nil {
			if err := sessionProvider.Close(); err != nil {
				_, _ = fmt.Fprintf(f.Stderr, "Warning: Failed to close session provider: %v\n", err)
				return err
			}
		}
		return nil
	}
	return sessionProvider, cleanup, nil
}

func (f *DefaultSessionFactory) applySessionSecuritySettings(ctx stdctx.Context, sessionProvider ports.SessionProvider) {
	if val, err := sessionProvider.GetSettings().Get(ctx, "bypass_confirmation"); err == nil && val == "true" {
		f.SM.SetBypassActive(true)
	}

	// Load authorized paths from settings
	loadPathsFromSettings(ctx, sessionProvider.GetSettings(), "authorized_safe_paths", f.SM.RegisterSafePath, f.Logger)
	loadPathsFromSettings(ctx, sessionProvider.GetSettings(), "authorized_read_paths", f.SM.RegisterReadOnlyPath, f.Logger)
}

func loadPathsFromSettings(ctx stdctx.Context, kv ports.KVStore, key string, register func(string), logger *slog.Logger) {
	val, err := kv.Get(ctx, key)
	if err != nil || val == "" {
		return
	}

	var paths []string
	if err := json.Unmarshal([]byte(val), &paths); err != nil {
		logger.Error("failed to unmarshal "+key, "error", err, "value", val)
		return
	}

	for _, p := range paths {
		register(p)
	}
}

func (f *DefaultSessionFactory) setupSecurity(paths *persistence.Paths, configPath string) error {
	f.SM.RegisterSafePath(filepath.Join(f.HomeDir, "output"))
	f.SM.RegisterReadOnlyPath(configPath)
	return nil
}

func (f *DefaultSessionFactory) handleNewSession(ctx stdctx.Context, paths *persistence.Paths, cfg *config.Config, pricingOverrides map[string]pricing.ModelPricing, kvStore ports.KVStore) error {
	timestamp := time.Now().Format("20060102_150405")
	uniqueID := fmt.Sprintf("backup/%s/%s", timestamp, filepath.Base(paths.LogPath))
	if err := telemetry.RecordSessionCost(ctx, f.SM, nil, paths.LogPath, cfg.Model, cfg.Mode, uniqueID, pricingOverrides); err != nil {
		_, _ = fmt.Fprintf(f.Stderr, "Warning: Failed to record session cost for backup (log may be missing/corrupt): %v\n", err)
	}

	// Critical path: always attempt to rotate the session
	retentionDays := 30
	if val, err := kvStore.Get(ctx, "backup_retention_days"); err == nil && val != "" {
		if parsed, err := strconv.Atoi(val); err == nil {
			retentionDays = parsed
		}
	}
	if err := f.RotateSession(f.FileSystem, f.Stdout, *paths, retentionDays); err != nil {
		return fmt.Errorf("session rotation failed: %w", err)
	}
	return nil
}

func (f *DefaultSessionFactory) BuildSession(ctx stdctx.Context, cfg *config.Config, configPath string, newSession bool, pricingOverrides map[string]pricing.ModelPricing) (ports.SessionProvider, *persistence.Paths, func(stdctx.Context) error, error) {
	paths := persistence.ResolvePaths(f.HomeDir, cfg.Mode)
	if err := infra_persistence.EnsureDirectories(f.FileSystem, paths); err != nil {
		return nil, nil, nil, err
	}

	if err := f.setupSecurity(paths, configPath); err != nil {
		return nil, nil, nil, err
	}

	sessionProvider, cleanup, err := f.buildSessionProvider(ctx, paths, cfg)
	if err != nil {
		return nil, nil, nil, err
	}

	f.applySessionSecuritySettings(ctx, sessionProvider)

	if newSession {
		if err := f.handleNewSession(ctx, paths, cfg, pricingOverrides, sessionProvider.GetSettings()); err != nil {
			_ = cleanup(ctx)
			return nil, nil, nil, fmt.Errorf("session initialization failed during rotation: %w", err)
		}
	}

	f.SM.SetCommandsLogFile(paths.CommandsLogPath)

	return sessionProvider, paths, cleanup, nil
}
