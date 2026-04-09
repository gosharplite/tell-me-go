package toolchain

import (
	"context"
	"errors"
	"os"
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

func TestGoRunner_RunTests_RaceFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		withRace bool
		wantRace bool
	}{
		{
			name:     "default (race enabled)",
			withRace: true,
			wantRace: true,
		},
		{
			name:     "explicitly enabled",
			withRace: true,
			wantRace: true,
		},
		{
			name:     "race disabled",
			withRace: false,
			wantRace: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedArgs []string
			mock := &mockExecutor{
				combinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
					capturedArgs = args
					return nil, nil
				},
			}

			var runner *GoRunner
			if tt.name == "default (race enabled)" {
				runner = NewGoRunner(mock)
			} else {
				runner = NewGoRunner(mock, WithRace(tt.withRace))
			}
			
			_, _ = runner.RunTests(context.Background(), "./...")

			hasRace := false
			for _, arg := range capturedArgs {
				if arg == "-race" {
					hasRace = true
					break
				}
			}

			if hasRace != tt.wantRace {
				t.Errorf("wantRace=%v, got %v in args: %v", tt.wantRace, hasRace, capturedArgs)
			}
		})
	}
}

func TestGoRunner_RunLinter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		setup    func(*mockExecutor)
		wantTool string
		wantErr  error
	}{
		{
			name: "golangci-lint found",
			setup: func(m *mockExecutor) {
				m.lookPathFunc = func(f string) (string, error) {
					if f == "golangci-lint" {
						return "/bin/golangci-lint", nil
					}
					return "", errors.New("not found")
				}
			},
			wantTool: "golangci-lint",
		},
		{
			name: "staticcheck fallback",
			setup: func(m *mockExecutor) {
				m.lookPathFunc = func(f string) (string, error) {
					if f == "staticcheck" {
						return "/bin/staticcheck", nil
					}
					return "", errors.New("not found")
				}
			},
			wantTool: "staticcheck",
		},
		{
			name: "no linter found",
			setup: func(m *mockExecutor) {
				m.lookPathFunc = func(f string) (string, error) {
					return "", errors.New("not found")
				}
			},
			wantErr: ErrNoSupportedLinter,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := &mockExecutor{}
			if tt.setup != nil {
				tt.setup(mock)
			}
			runner := NewGoRunner(mock)
			_, toolUsed, err := runner.RunLinter(context.Background())

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("wantErr=%v, got err=%v", tt.wantErr, err)
			}
			if toolUsed != tt.wantTool {
				t.Errorf("wantTool=%q, got %q", tt.wantTool, toolUsed)
			}
		})
	}
}

func TestGoRunner_BuildCode(t *testing.T) {
	t.Parallel()
	mock := &mockExecutor{
		combinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if name == "go" && args[0] == "build" {
				return []byte("success"), nil
			}
			return nil, errors.New("unexpected command")
		},
	}
	runner := NewGoRunner(mock)
	out, err := runner.BuildCode(context.Background(), "app", "./main.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != "success" {
		t.Errorf("expected success, got %s", string(out))
	}
}

func TestGoRunner_CheckGovulncheck(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		found   bool
		wantErr bool
	}{
		{
			name:    "installed",
			found:   true,
			wantErr: false,
		},
		{
			name:    "not installed",
			found:   false,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := &mockExecutor{
				lookPathFunc: func(f string) (string, error) {
					if tt.found && f == "govulncheck" {
						return "/bin/govulncheck", nil
					}
					return "", errors.New("not found")
				},
			}
			runner := NewGoRunner(mock)
			err := runner.CheckGovulncheck(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("wantErr=%v, got err=%v", tt.wantErr, err)
			}
		})
	}
}

func TestGoRunner_RunModTidy(t *testing.T) {
	t.Parallel()
	mock := &mockExecutor{
		combinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if name == "go" && args[0] == "mod" && args[1] == "tidy" {
				return []byte("tidy"), nil
			}
			return nil, errors.New("unexpected command")
		},
	}
	runner := NewGoRunner(mock)
	out, err := runner.RunModTidy(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != "tidy" {
		t.Errorf("expected tidy, got %s", string(out))
	}
}

func TestGoRunner_FormatCode(t *testing.T) {
	t.Parallel()
	mock := &mockExecutor{
		combinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if name == "go" && args[0] == "fmt" {
				return []byte("formatted"), nil
			}
			return nil, errors.New("unexpected command")
		},
	}
	runner := NewGoRunner(mock)
	out, err := runner.FormatCode(context.Background(), "./...")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != "formatted" {
		t.Errorf("expected formatted, got %s", string(out))
	}
}

func TestGoRunner_GetPackageList(t *testing.T) {
	t.Parallel()
	mock := &mockExecutor{
		combinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if name == "go" && args[0] == "list" && args[1] == "-json" {
				return []byte(`{"Name": "main"}`), nil
			}
			return nil, errors.New("unexpected command")
		},
	}
	runner := NewGoRunner(mock)
	out, err := runner.GetPackageList(context.Background(), "./...")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(out), "main") {
		t.Errorf("expected json, got %s", string(out))
	}
}

func TestGoRunner_GetGoDoc(t *testing.T) {
	t.Parallel()
	mock := &mockExecutor{
		combinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if name == "go" && args[0] == "doc" {
				return []byte("doc content"), nil
			}
			return nil, errors.New("unexpected command")
		},
	}
	runner := NewGoRunner(mock)
	out, err := runner.GetGoDoc(context.Background(), "fmt.Println")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != "doc content" {
		t.Errorf("expected doc content, got %s", string(out))
	}
}

func TestGoRunner_GetModulePath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		out     string
		err     error
		want    string
		wantErr bool
	}{
		{
			name: "success",
			out:  "github.com/org/repo\n",
			want: "github.com/org/repo",
		},
		{
			name:    "failure",
			err:     errors.New("not a module"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := &mockExecutor{
				combinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
					if name == "go" && args[0] == "list" && args[1] == "-m" {
						return []byte(tt.out), tt.err
					}
					return nil, nil
				},
			}
			runner := NewGoRunner(mock)
			res, err := runner.GetModulePath(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("wantErr=%v, got err=%v", tt.wantErr, err)
			}
			if res != tt.want {
				t.Errorf("want=%q, got %q", tt.want, res)
			}
		})
	}
}

func TestGoRunner_GetModuleDir(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		out     string
		err     error
		want    string
		wantErr bool
	}{
		{
			name: "success",
			out:  "/home/user/repo\n",
			want: "/home/user/repo",
		},
		{
			name:    "failure",
			err:     errors.New("not a module"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := &mockExecutor{
				combinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
					if name == "go" && args[0] == "list" && args[1] == "-m" && args[2] == "-f" {
						return []byte(tt.out), tt.err
					}
					return nil, nil
				},
			}
			runner := NewGoRunner(mock)
			res, err := runner.GetModuleDir(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("wantErr=%v, got err=%v", tt.wantErr, err)
			}
			if res != tt.want {
				t.Errorf("want=%q, got %q", tt.want, res)
			}
		})
	}
}

func TestRunTestsWithCoverage_Options(t *testing.T) {
	t.Run("short and profilePath", func(t *testing.T) {
		var capturedArgs []string
		mock := &mockExecutor{
			combinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if name == "go" && len(args) > 0 && args[0] == "test" {
					capturedArgs = args
				}
				if len(args) > 0 && args[0] == "test" {
					return []byte("ok"), nil
				}
				if len(args) > 1 && args[0] == "tool" && args[1] == "cover" {
					return []byte("total: (statements) 50.0%"), nil
				}
				return nil, nil
			},
		}
		runner := NewGoRunner(mock)
		report, err := runner.RunTestsWithCoverage(context.Background(), "./...", true, "custom.out")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		
		foundShort := false
		foundProfile := false
		for _, arg := range capturedArgs {
			if arg == "-short" {
				foundShort = true
			}
			if strings.HasPrefix(arg, "-coverprofile=custom.out") {
				foundProfile = true
			}
		}
		if !foundShort {
			t.Error("expected -short flag")
		}
		if !foundProfile {
			t.Error("expected -coverprofile=custom.out")
		}
		if report.CoveragePct != "50.0%" {
			t.Errorf("expected 50.0%%, got %s", report.CoveragePct)
		}
	})

	t.Run("unmatched regex", func(t *testing.T) {
		mock := &mockExecutor{
			combinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if len(args) > 0 && args[0] == "test" { return []byte("ok"), nil }
				if len(args) > 1 && args[0] == "tool" && args[1] == "cover" { return []byte("no match"), nil }
				return nil, nil
			},
		}
		runner := NewGoRunner(mock)
		report, _ := runner.RunTestsWithCoverage(context.Background(), "./...", false, "")
		if report.CoveragePct != "N/A" {
			t.Errorf("expected N/A, got %s", report.CoveragePct)
		}
	})
}

func TestRunTestsWithCoverage_CreateTempError(t *testing.T) {
	t.Parallel()
	mock := &mockExecutor{}

	// [REFACTOR] Use the consistent Functional Option pattern
	failCreate := func(string, string) (*os.File, error) {
		return nil, errors.New("disk full")
	}
	runner := NewGoRunner(mock, WithFilesystem(failCreate, os.Remove))

	_, err := runner.RunTestsWithCoverage(context.Background(), "./...", false, "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to create temp coverage file: disk full") {
		t.Errorf("unexpected error message: %v", err)
	}
}
