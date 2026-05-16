// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysistest

// Barrier synchronizes N goroutines at a release point.
// Each goroutine calls ready <- struct{}{} to signal arrival,
// then <-release to wait. Call Barrier(N) and close the returned
// release channel to release all goroutines simultaneously.
func Barrier(N int) (ready chan<- struct{}, release chan struct{}) {
	r := make(chan struct{}, N)
	rel := make(chan struct{})
	return r, rel
}
