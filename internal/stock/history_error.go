package stock

import (
	"errors"
	"fmt"
	"net"
)

// Keep upstream text available to internal callers; only HistoryReadFailure is public diagnostic text.
type historyReadError struct {
	err      error
	category string
	status   int
}

func (e *historyReadError) Error() string { return e.err.Error() }
func (e *historyReadError) Unwrap() error { return e.err }

func historyFailure(err error, status int, semantic bool) error {
	if err == nil {
		return nil
	}
	var existing *historyReadError
	if errors.As(err, &existing) {
		return err
	}
	category := "connection"
	var timeout net.Error
	switch {
	case semantic:
		category = "invalid_history"
	case errors.As(err, &timeout) && timeout.Timeout():
		category = "timeout"
	case status == 401:
		category = "unauthorized"
	case status == 403:
		category = "forbidden"
	case status >= 200 && status < 300:
		category = "invalid_response"
	case status != 0:
		category = "http_error"
	}
	return &historyReadError{err: err, category: category, status: status}
}

// HistoryReadFailure returns fixed categories and a numeric status, never upstream text or request data.
func HistoryReadFailure(err error) string {
	var failure *historyReadError
	if !errors.As(err, &failure) {
		return "unavailable"
	}
	category := failure.category
	switch category {
	case "connection", "timeout", "unauthorized", "forbidden", "invalid_response", "invalid_history", "http_error":
	default:
		return "unavailable"
	}
	if failure.status >= 100 && failure.status <= 599 {
		return fmt.Sprintf("%s; HTTP %d", category, failure.status)
	}
	return category
}
