package lystok

import (
	"fmt"
	"time"
)

// Issue is one problem in the request body, e.g. {Path: "to", Message: "Invalid email"}.
type Issue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// Error is anything the API refused, or a request that never got an answer. Find it with
// errors.As:
//
//	var apiErr *lystok.Error
//	if errors.As(err, &apiErr) && apiErr.Code == "validation" { … }
//
// Code is stable and meant for your code (validation, not_found, rate_limited, plan_limit, …);
// Message is for people. Without an answer from the API, Status is 0 and Code is
// connection_error or timeout.
type Error struct {
	// HTTP status, or 0 when no answer came.
	Status  int
	Code    string
	Message string
	// Values behind the message, such as a limit.
	Params map[string]any
	// Field problems of a validation error.
	Issues []Issue
	// Quote it when you write to support.
	RequestID string
	// How long the API asked to wait before trying again; 0 when it did not say.
	RetryAfter time.Duration

	cause error
}

func (e *Error) Error() string {
	return fmt.Sprintf("lystok: %s (status %d, code %s)", e.Message, e.Status, e.Code)
}

// Unwrap returns the network error behind a request that got no answer.
func (e *Error) Unwrap() error { return e.cause }

// WebhookVerificationError is a webhook whose signature or timestamp is wrong: answer it with
// 401 and do not trust it.
type WebhookVerificationError struct {
	Message string
}

func (e *WebhookVerificationError) Error() string { return "lystok: " + e.Message }
