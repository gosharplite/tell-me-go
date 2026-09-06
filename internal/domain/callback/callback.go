// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package callback

import "context"

const (
	StatusSuccess = "success"
	StatusError   = "error"
)

// CallbackPayload represents the closed wire contract sent to the callback URL.
type CallbackPayload struct {
	SessionID string  `json:"session_id"`
	Status    string  `json:"status"`   // "success" | "error"
	Response  string  `json:"response"` // non-empty iff status == "success"
	Error     *string `json:"error"`    // string iff status == "error", else null
}

// CallbackNotifier defines the port for delivering terminal session webhooks.
type CallbackNotifier interface {
	Notify(ctx context.Context, url string, headers map[string]string, payload CallbackPayload) error
}
