package toolchain

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type mockExecutor struct {
	combinedOutputFunc func(ctx context.Context, name string, args ...string) ([]byte, error)
}

func (m *mockExecutor) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return m.combinedOutputFunc(ctx, name, args...)
}

func (m *mockExecutor) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.combinedOutputFunc != nil {
		return m.combinedOutputFunc(ctx, name, args...)
	}
	return nil, nil
}

func TestRunTestsWithCoverage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		testOut     string
		testErr     error
		sumOut      string
		sumErr      error
		wantSum     string
		wantPct     string
		wantNoGo    bool
		wantErr     bool
	}{
		{
			name:    "success",
			testOut: "ok package1\nok package2",
			sumOut:  "total: (statements) 85.0%",
			wantPct: "85.0%",
			wantSum: "total: (statements) 85.0%",
		},
		{
			name:    "no go files",
			testOut: "?   \tgithub.com/pkg\t[no test files]\n. no Go files",
			testErr: errors.New("exit status 1"),
			wantPct: "0.0%",
			wantNoGo: true,
		},
		{
			name:    "test failure",
			testOut: "FAIL",
			testErr: errors.New("exit status 1"),
			wantErr: true,
		},
		{
			name:    "summary failure",
			testOut: "ok",
			sumErr:  errors.New("failed"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := &mockExecutor{
				combinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
					if args[0] == "test" {
						return []byte(tt.testOut), tt.testErr
					}
					if args[0] == "tool" && args[1] == "cover" {
						return []byte(tt.sumOut), tt.sumErr
					}
					return nil, nil
				},
			}
			runner := NewGoRunner(mock)
			report, err := runner.RunTestsWithCoverage(context.Background(), "./...", false, "")

			if (err != nil) != tt.wantErr {
				t.Errorf("wantErr=%v, got err=%v", tt.wantErr, err)
			}
			if !tt.wantErr {
				if report.CoveragePct != tt.wantPct {
					t.Errorf("wantPct=%q, got %q", tt.wantPct, report.CoveragePct)
				}
				if report.NoGoFiles != tt.wantNoGo {
					t.Errorf("wantNoGo=%v, got %v", tt.wantNoGo, report.NoGoFiles)
				}
			}
		})
	}
}

func TestRunBenchmarks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		out     string
		err     error
		wantSub string
		wantErr bool
	}{
		{
			name:    "success",
			out:     "BenchmarkResult 1000",
			wantSub: "BenchmarkResult",
		},
		{
			name:    "no go files",
			out:     "no Go files",
			err:     errors.New("exit status 1"),
			wantSub: "No Go files found in target path",
		},
		{
			name:    "failure",
			out:     "error",
			err:     errors.New("exit status 1"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := &mockExecutor{
				combinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
					return []byte(tt.out), tt.err
				},
			}
			runner := NewGoRunner(mock)
			res, err := runner.RunBenchmarks(context.Background(), "./...", ".")

			if (err != nil) != tt.wantErr {
				t.Errorf("wantErr=%v, got err=%v", tt.wantErr, err)
			}
			if !tt.wantErr {
				if !strings.Contains(res, tt.wantSub) {
					t.Errorf("wantSub=%q, got %q", tt.wantSub, res)
				}
			}
		})
	}
}