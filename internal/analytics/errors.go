package analytics

import (
	"context"
	"errors"
	"fmt"

	"chatwoot-mcp/internal/chatwoot"
)

// Code is a machine-readable analytics failure category. Wire codes match the
// operational service plus unsupported_feature for methods whose upstream
// surface is not wired yet.
type Code string

const (
	CodeInvalidInput       Code = "invalid_input"
	CodeUnauthorized       Code = "unauthorized"
	CodeForbidden          Code = "forbidden"
	CodeNotFound           Code = "not_found"
	CodeRateLimited        Code = "rate_limited"
	CodeTimeout            Code = "timeout"
	CodeUpstream           Code = "upstream_error"
	CodeUpstreamServer     Code = "upstream_server_error"
	CodeInvalidResponse    Code = "invalid_response"
	CodeUnsupportedFeature Code = "unsupported_feature"
)

// Error is a typed analytics failure. It wraps the originating Chatwoot error,
// when there is one, so callers can read both the category and the original
// API classification. It never carries the token or raw upstream body text.
type Error struct {
	Code     Code
	Message  string
	APIError *chatwoot.Error
}

func (e *Error) Error() string {
	if e.APIError != nil {
		return fmt.Sprintf("analytics: %s: %v", e.Code, e.APIError)
	}
	if e.Message != "" {
		return fmt.Sprintf("analytics: %s: %s", e.Code, e.Message)
	}
	return "analytics: " + string(e.Code)
}

// Unwrap exposes the underlying Chatwoot error to errors.As and errors.Is.
func (e *Error) Unwrap() error {
	if e == nil || e.APIError == nil {
		return nil
	}
	return e.APIError
}

// CodeOf reports the machine-readable code of err, or "" when err is not an
// analytics error.
func CodeOf(err error) Code {
	var analyticsErr *Error
	if errors.As(err, &analyticsErr) {
		return analyticsErr.Code
	}
	return ""
}

func invalidInput(message string) error {
	return &Error{Code: CodeInvalidInput, Message: message}
}

func codeForKind(kind chatwoot.Kind) Code {
	switch kind {
	case chatwoot.KindUnauthorized:
		return CodeUnauthorized
	case chatwoot.KindForbidden:
		return CodeForbidden
	case chatwoot.KindNotFound:
		return CodeNotFound
	case chatwoot.KindRateLimited:
		return CodeRateLimited
	case chatwoot.KindTimeout:
		return CodeTimeout
	case chatwoot.KindServer:
		return CodeUpstreamServer
	case chatwoot.KindInvalid:
		return CodeInvalidResponse
	default:
		return CodeUpstream
	}
}

func mapSourceError(err error) error {
	var apiErr *chatwoot.Error
	if errors.As(err, &apiErr) {
		return &Error{Code: codeForKind(apiErr.Kind), Message: apiErr.Message, APIError: apiErr}
	}
	return &Error{Code: CodeUpstream, Message: err.Error()}
}

func mapReportError(err error, unsupported404 bool) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &Error{Code: CodeTimeout, Message: "report request was cancelled or timed out"}
	}
	var apiErr *chatwoot.Error
	if errors.As(err, &apiErr) {
		if unsupported404 && apiErr.Kind == chatwoot.KindNotFound {
			return &Error{Code: CodeUnsupportedFeature, Message: "Chatwoot report endpoint is unavailable", APIError: apiErr}
		}
		return &Error{Code: codeForKind(apiErr.Kind), Message: apiErr.Message, APIError: apiErr}
	}
	return &Error{Code: CodeUpstream, Message: err.Error()}
}
