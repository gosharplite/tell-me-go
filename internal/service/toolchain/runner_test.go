package toolchain

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type mockExecutor struct {
	combinedOutputFunc func(ctx context.Context, name string, args ...string) ([]byte, error)
	lookPathFunc       func(file string) (string, error)
}

func (m *mockExecutor) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.combinedOutputFunc != nil {
		return m.combinedOutputFunc(ctx, name, args...)
	}
	return nil, nil
}

func (m *mockExecutor) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.combinedOutputFunc != nil {
		return m.combinedOutputFunc(ctx, name, args...)
	}
	return nil, nil
}

func (m *mockExecutor) LookPath(file string) (string, error) {
	if m.lookPathFunc != nil {
		return m.lookPathFunc(file)
	}
	return "/usr/bin/" + file, nil
}

func TestRunTestsWithCoverage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		testOut  string
		testErr  error
		sumOut   string
		sumErr   error
		wantSum  string
		wantPct  string
		wantNoGo bool
		wantErr  bool
	}{
		{
			name:    "success",
			testOut: "ok package1\nok package2",
			sumOut:  "total: (statements) 85.0%",
			wantPct: "85.0%",
			wantSum: "total: (statements) 85.0%",
		},
		{
			name:     "no go files",
			testOut:  "?   \tgithub.com/pkg\t[no test files]\n. no Go files",
			testErr:  errors.New("exit status 1"),
			wantPct:  "0.0%",
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

func TestGoRunner_Timeout(t *testing.T) {
	mock := &mockExecutor{
		combinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	runner := NewGoRunner(mock)

	// Use a background context; the runner's internal 30s timeout will eventually fire, 
	// but we don't want to wait 30s in a unit test.
	// Instead, provide a context that is ALREADY cancelled or has a very short deadline
	// to verify the runner handles context propagation correctly.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	_, err := runner.RunTests(ctx, "./...")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
}

func TestGoRunner_DefaultTimeout(t *testing.T) {
	mock := &mockExecutor{
		combinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Error("expected deadline to be set by GoRunner")
				return nil, errors.New("no deadline")
			}
			
			// The deadline should be approximately now + defaultTimeout
			expectedDeadline := time.Now().Add(100 * time.Millisecond)
			if deadline.Before(expectedDeadline.Add(-50*time.Millisecond)) || deadline.After(expectedDeadline.Add(50*time.Millisecond)) {
				t.Errorf("unexpected deadline: got %v, want approx %v", deadline, expectedDeadline)
			}
			
			return nil, nil
		},
	}
	
	runner := NewGoRunner(mock, WithDefaultTimeout(100*time.Millisecond))
	
	_, err := runner.RunTests(context.Background(), "./...")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGoRunner_RespectsExistingDeadline(t *testing.T) {
	mock := &mockExecutor{
		combinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Error("expected deadline to be set")
				return nil, errors.New("no deadline")
			}
			
			// The deadline should be the one we set in the test, not the default
			expectedDeadline := time.Now().Add(1 * time.Second)
			if deadline.After(expectedDeadline.Add(100*time.Millisecond)) {
				t.Errorf("deadline was too far in the future: got %v, want approx %v", deadline, expectedDeadline)
			}
			
			return nil, nil
		},
	}
	
	// Set a very long default timeout that should be ignored
	runner := NewGoRunner(mock, WithDefaultTimeout(1*time.Hour))
	
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	
	_, err := runner.RunTests(ctx, "./...")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
