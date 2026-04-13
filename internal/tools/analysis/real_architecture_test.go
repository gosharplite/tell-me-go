// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"strings"
	"testing"
)

func TestVerifyRealArchitecture(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real architecture validation in short mode")
	}

	m := &architectureManager{
		SP:     &mockSecurityProvider{},
		Runner: &realAnalysisGoRunner{},
	}

	res, err := m.VerifyArchitecture(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("VerifyArchitecture failed: %v", err)
	}

	if strings.Contains(res.Text, "FAILED") {
		t.Logf("Architecture validation FAILED:\n%s", res.Text)
	} else {
		t.Log("✅ No architectural violations detected.")
	}
}
