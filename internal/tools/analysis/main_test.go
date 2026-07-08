package analysis

import (
	"fmt"
	"os"
	"testing"
)

// testMainTB is a minimal implementation of testing.TB that only supports Fatalf,
// which is all that getSharedIndexer needs if it fails.
type testMainTB struct {
	testing.TB
}

func (testMainTB) Fatalf(format string, args ...any) {
	fmt.Printf("FATAL: "+format+"\n", args...)
	os.Exit(1)
}

func TestMain(m *testing.M) {
	// Prime the shared indexer before any parallel tests run.
	// This avoids loadMu contention between the large full-project
	// packages.Load and the many small temp-workspace packages.Load
	// calls that happen during parallel test execution.
	_ = getSharedIndexerForTestMain()

	os.Exit(m.Run())
}

// getSharedIndexerForTestMain primes the shared indexer for TestMain.
func getSharedIndexerForTestMain() *indexer {
	return getSharedIndexer(testMainTB{})
}
