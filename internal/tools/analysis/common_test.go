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
func (s *mockSecurityProvider) Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
	return true, nil
}

type MockExecutor struct {
	OutputFunc         func(ctx context.Context, name string, args ...string) ([]byte, error)
	CombinedOutputFunc func(ctx context.Context, name string, args ...string) ([]byte, error)
}

func (m *MockExecutor) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.OutputFunc != nil {
		return m.OutputFunc(ctx, name, args...)
	}
	return nil, nil
}

func (m *MockExecutor) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.CombinedOutputFunc != nil {
		return m.CombinedOutputFunc(ctx, name, args...)
	}
	return nil, nil
}

func (s *mockSecurityProvider) GetPolicy() *domain.Policy {
	return domain.DefaultPolicy()
}

func (s *mockSecurityProvider) GetSafetyService() *domain.SafetyService {
	return domain.NewSafetyService(domain.DefaultPolicy())
}
