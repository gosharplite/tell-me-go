package analysis

import (
	"context"

	domain "github.com/gosharplite/tell-me-go/internal/domain/security"
)

type mockSecurityProvider struct{}

func (s *mockSecurityProvider) IsPathSafe(path string) (string, error)     { return path, nil }
func (s *mockSecurityProvider) IsPathWritable(path string) (string, error) { return path, nil }
func (s *mockSecurityProvider) ConfirmDestructiveAction(ctx context.Context, action, target, detail string) (bool, error) {
	return true, nil
}
func (s *mockSecurityProvider) TerminalLock()                              {}
func (s *mockSecurityProvider) TerminalUnlock()                            {}
func (s *mockSecurityProvider) IsCommandAllowed(command string) bool       { return true }
func (s *mockSecurityProvider) LogAudit(label1, val1, label2, val2 string) {}
func (s *mockSecurityProvider) IsBypassActive() bool                       { return false }
func (s *mockSecurityProvider) Prompt(message string)                      {}
func (s *mockSecurityProvider) Warn(message string)                        {}
func (s *mockSecurityProvider) ReadLine(ctx context.Context) (string, error) {
	return "", nil
}
func (s *mockSecurityProvider) Confirm(ctx context.Context, message string) (bool, error) {
	return true, nil
}
func (s *mockSecurityProvider) Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
	return true, nil
}

type mockExecutor struct {
	OutputFunc         func(ctx context.Context, name string, args ...string) ([]byte, error)
	CombinedOutputFunc func(ctx context.Context, name string, args ...string) ([]byte, error)
}

func (m *mockExecutor) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.OutputFunc != nil {
		return m.OutputFunc(ctx, name, args...)
	}
	return nil, nil
}

func (m *mockExecutor) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.CombinedOutputFunc != nil {
		return m.CombinedOutputFunc(ctx, name, args...)
	}
	return nil, nil
}

func (s *mockSecurityProvider) GetSafetyService() *domain.SafetyService {
	return domain.NewSafetyService(domain.DefaultPolicy())
}
