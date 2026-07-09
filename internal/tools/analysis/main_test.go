package analysis

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	code := m.Run()
	// Clean up shared temp dirs used by dead_code_deep_test.go
	// (created via os.MkdirTemp and not tied to any test's t.TempDir).
	if sharedDeepIdentWS.dir != "" {
		_ = os.RemoveAll(sharedDeepIdentWS.dir)
	}
	if sharedDeepLimWS.dir != "" {
		_ = os.RemoveAll(sharedDeepLimWS.dir)
	}
	os.Exit(code)
}
