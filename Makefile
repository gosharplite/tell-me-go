# Copyright (c) 2026 gosharplite@gmail.com
# SPDX-License-Identifier: MIT

.PHONY: test-coverage
test-coverage:
	go test -coverprofile=coverage.raw ./...
	grep -v -E "mock\.go|generated" coverage.raw > coverage.out
	go tool cover -func=coverage.out
