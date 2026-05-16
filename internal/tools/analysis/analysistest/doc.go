// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package analysistest provides shared test infrastructure for the analysis sub-packages
// (singleflight, methodset, deadcode), as described in ADR-022.
//
// It contains configurable mocks (MockSymbolIndex, MockSecurityProvider) that were previously
// duplicated across multiple test files in the parent analysis package.
package analysistest
