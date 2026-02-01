package analysis

import (
	"context"
)

type mockSecurityProvider struct{}

func (s *mockSecurityProvider) IsPathSafe(path string) (string, error) { return path, nil }
func (s *mockSecurityProvider) IsPathWritable(path string) (string, error) { return path, nil }
func (s *mockSecurityProvider) ConfirmDestructiveAction(ctx context.Context, action, target, detail string) (bool, error) {
	return true, nil
}
func (s *mockSecurityProvider) TerminalLock()   {}
func (s *mockSecurityProvider) TerminalUnlock() {}
