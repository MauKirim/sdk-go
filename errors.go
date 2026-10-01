package maukirim

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// Error is the failure returned by every SDK call. Failures reported by the
// API carry its code and HTTP status; transport failures use StatusCode 0.
//
//	var apiErr *maukirim.Error
//	if errors.As(err, &apiErr) && apiErr.Code == "insufficient_credits" {
//		topUp()
//	}
type Error struct {
	// StatusCode is the HTTP status of the response, or 0 for a transport
	// failure (connection refused, timeout, …).
	StatusCode int
	// Code is the API error code, e.g. "api_key_invalid", "worker_offline".
	Code string
	// Message is a human readable description.
	Message string

	cause error
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.StatusCode == 0 {
		return fmt.Sprintf("maukirim: %s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("maukirim: %s (HTTP %d): %s", e.Code, e.StatusCode, e.Message)
}

// Unwrap exposes the underlying failure, if any.
func (e *Error) Unwrap() error { return e.cause }

// Retryable reports whether repeating the request could succeed. It is true for
// 500/502/503 responses and for transport failures and timeouts, and false for
// every 4xx (including 429, which should be retried later, not immediately).
func (e *Error) Retryable() bool {
	if e == nil {
		return false
	}
	switch e.StatusCode {
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable:
		return true
	case 0:
		return e.Code == "network_error" || e.Code == "timeout"
	}
	return false
}

// ErrMissingIdempotencyKey is returned by the three endpoints that require an
// Idempotency-Key when the caller supplies an empty key. No request is made.
var ErrMissingIdempotencyKey = &Error{
	Code:    "idempotency_key_required",
	Message: "this endpoint requires a non-empty IdempotencyKey",
}

// requireIdempotencyKey returns ErrMissingIdempotencyKey when key is blank.
func requireIdempotencyKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return ErrMissingIdempotencyKey
	}
	return nil
}

// transportError classifies a failure that happened before a response arrived.
func transportError(err error) *Error {
	code := "network_error"
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		code = "timeout"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		code = "timeout"
	}
	return &Error{Code: code, Message: err.Error(), cause: err}
}

// decodeError turns a non-2xx response into an *Error.
func decodeError(status int, body []byte) *Error {
	var envelope struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	_ = json.Unmarshal(body, &envelope)

	message := envelope.Message
	if message == "" {
		message = envelope.Error
	}
	if message == "" {
		message = strings.TrimSpace(string(body))
	}
	if message == "" {
		message = http.StatusText(status)
	}

	code := envelope.Code
	if code == "" {
		code = "unknown_error"
	}
	if len(message) > 512 {
		message = message[:512]
	}
	return &Error{StatusCode: status, Code: code, Message: message}
}
