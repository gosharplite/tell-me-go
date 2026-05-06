package session

import (
	"os"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
)

// stubFileInfo implements os.FileInfo for testing.
type stubFileInfo struct{ modTime time.Time }

func (s stubFileInfo) Name() string       { return "stub" }
func (s stubFileInfo) Size() int64        { return 0 }
func (s stubFileInfo) Mode() os.FileMode  { return 0 }
func (s stubFileInfo) ModTime() time.Time { return s.modTime }
func (s stubFileInfo) IsDir() bool        { return false }
func (s stubFileInfo) Sys() interface{}   { return nil }

// stubFileStat implements FileStat for testing.
type stubFileStat struct {
	statErr error
	modTime time.Time
}

func (s stubFileStat) Stat(name string) (os.FileInfo, error) {
	if s.statErr != nil {
		return nil, s.statErr
	}
	return stubFileInfo{modTime: s.modTime}, nil
}

// mockConfigLoader implements config.ConfigLoader for internal tests.
type mockConfigLoader struct{}

func (mockConfigLoader) Load(path string) (*config.Config, error) {
	return &config.Config{
		MaxHistoryTokens: 10000,
		MaxToolTurns:     20,
		MaxHistoryTurns:  50,
	}, nil
}

func TestShouldReloadMain(t *testing.T) {
	pastTime := time.Now().Add(-1 * time.Hour)
	futureTime := time.Now().Add(1 * time.Hour)

	tests := []struct {
		name   string
		setup  func(cw *FileConfigWatcher)
		model  string
		wantOK bool
	}{
		{
			name: "empty path returns false",
			setup: func(cw *FileConfigWatcher) {
				cw.SetPaths("", "")
			},
			model:  "gpt-5",
			wantOK: false,
		},
		{
			name: "stat error returns false",
			setup: func(cw *FileConfigWatcher) {
				cw.SetPaths("/fake/main.yaml", "")
				cw.FS = stubFileStat{statErr: os.ErrNotExist}
			},
			model:  "gpt-5",
			wantOK: false,
		},
		{
			name: "unchanged mod time and same model returns false",
			setup: func(cw *FileConfigWatcher) {
				cw.SetPaths("/fake/main.yaml", "")
				cw.FS = stubFileStat{modTime: pastTime}
			},
			model:  "gpt-5",
			wantOK: false,
		},
		{
			name: "newer mod time returns true",
			setup: func(cw *FileConfigWatcher) {
				cw.SetPaths("/fake/main.yaml", "")
				cw.FS = stubFileStat{modTime: futureTime}
				cw.Loader = mockConfigLoader{}
			},
			model:  "gpt-5",
			wantOK: true,
		},
		{
			name: "same mod time but different model returns true",
			setup: func(cw *FileConfigWatcher) {
				cw.SetPaths("/fake/main.yaml", "")
				cw.FS = stubFileStat{modTime: pastTime}
				cw.Loader = mockConfigLoader{}
			},
			model:  "gpt-4",
			wantOK: true,
		},
		{
			name: "nil Loader returns false",
			setup: func(cw *FileConfigWatcher) {
				cw.SetPaths("/fake/main.yaml", "")
				cw.FS = stubFileStat{modTime: futureTime}
				cw.Loader = nil
			},
			model:  "gpt-5",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fcw := NewFileConfigWatcher(
				nil, nil, 100, 10, 20, nil,
			).(*FileConfigWatcher)

			tt.setup(fcw)

			if tt.name == "unchanged mod time and same model returns false" ||
				tt.name == "same mod time but different model returns true" {
				fcw.Refresh("gpt-5")
			}

			ok, info := fcw.shouldReloadMain(tt.model)
			if ok != tt.wantOK {
				t.Errorf("shouldReloadMain(%q) = (%v, %v); want ok=%v", tt.model, ok, info, tt.wantOK)
			}
		})
	}
}

func TestResolveContextWindow(t *testing.T) {
	const defaultWindow = 1000000

	tests := []struct {
		name          string
		cfg           *config.Config
		model         string
		defaultWindow int
		want          int
	}{
		{
			name: "model present with positive window returns override",
			cfg: &config.Config{
				Models: map[string]config.ModelConfig{
					"gpt-5": {ContextWindow: 128000},
				},
			},
			model:         "gpt-5",
			defaultWindow: defaultWindow,
			want:          128000,
		},
		{
			name: "model present with zero window falls back to default",
			cfg: &config.Config{
				Models: map[string]config.ModelConfig{
					"gpt-5": {ContextWindow: 0},
				},
			},
			model:         "gpt-5",
			defaultWindow: defaultWindow,
			want:          defaultWindow,
		},
		{
			name: "model present with negative window falls back to default",
			cfg: &config.Config{
				Models: map[string]config.ModelConfig{
					"gpt-5": {ContextWindow: -1},
				},
			},
			model:         "gpt-5",
			defaultWindow: defaultWindow,
			want:          defaultWindow,
		},
		{
			name: "model not found returns default",
			cfg: &config.Config{
				Models: map[string]config.ModelConfig{
					"other-model": {ContextWindow: 64000},
				},
			},
			model:         "gpt-5",
			defaultWindow: defaultWindow,
			want:          defaultWindow,
		},
		{
			name:          "nil Models map returns default",
			cfg:           &config.Config{},
			model:         "gpt-5",
			defaultWindow: defaultWindow,
			want:          defaultWindow,
		},
		{
			name: "custom default window is used when model absent",
			cfg: &config.Config{
				Models: map[string]config.ModelConfig{},
			},
			model:         "gpt-5",
			defaultWindow: 500000,
			want:          500000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveContextWindow(tt.cfg, tt.model, tt.defaultWindow)
			if got != tt.want {
				t.Errorf("resolveContextWindow(model=%q, default=%d) = %d; want %d",
					tt.model, tt.defaultWindow, got, tt.want)
			}
		})
	}
}
