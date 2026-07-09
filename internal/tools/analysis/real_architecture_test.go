// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"flag"
	"strings"
	"testing"
)

var strictArch = flag.Bool("strict-arch", true, "fail test on architecture violations")

func TestVerifyRealArchitecture(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real architecture validation in short mode")
	}

	m := &architectureManager{
		SP:  &mockSecurityProvider{},
		idx: getRealArchitectureIndexer(t),
	}

	res, err := m.VerifyArchitecture(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("VerifyArchitecture failed: %v", err)
	}

	if strings.Contains(res.Text, "FAILED") {
		if *strictArch {
			t.Errorf("Architecture validation FAILED:\n%s", res.Text)
		} else {
			t.Logf("Architecture validation FAILED:\n%s", res.Text)
		}
	}
}
