package apperr

import (
	"errors"
	"fmt"
)

// Code is a machine-readable error code for API responses.
type Code string

const (
	CodeNotFound         Code = "not_found"
	CodeConflict         Code = "conflict"
	CodeInvalidState     Code = "invalid_state"
	CodeValidationFailed Code = "validation_failed"
	CodeQuotaExceeded    Code = "quota_exceeded"
	CodeDeadlineExceeded Code = "deadline_exceeded"
	CodeLeaseExpired     Code = "lease_expired"
	CodePreempted        Code = "preempted"
	CodeDuplicate        Code = "duplicate"
	CodeInternal         Code = "internal"
	CodeUnavailable      Code = "unavailable"
	CodeForbidden        Code = "forbidden"
	CodeUnauthorized     Code = "unauthorized"
)

// Error is the domain error type carrying a code and a wrapped cause.
type Error struct {
	Code    Code
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error {
	return e.Cause
}

// New creates a new error without a cause.
func New(code Code, msg string) *Error {
	return &Error{Code: code, Message: msg}
}

// Wrap creates a new error wrapping an existing cause.
func Wrap(code Code, msg string, cause error) *Error {
	return &Error{Code: code, Message: msg, Cause: cause}
}

// NotFound returns a not-found error.
func NotFound(entity, id string) *Error {
	return &Error{Code: CodeNotFound, Message: fmt.Sprintf("%s not found: %s", entity, id)}
}

// Conflict returns an optimistic-lock conflict error.
func Conflict(entity, id string, version int) *Error {
	return &Error{
		Code:    CodeConflict,
		Message: fmt.Sprintf("%s %s version conflict at %d", entity, id, version),
	}
}

// InvalidTransition returns an illegal state-transition error.
func InvalidTransition(entity, from, to string) *Error {
	return &Error{
		Code:    CodeInvalidState,
		Message: fmt.Sprintf("illegal transition for %s: %s -> %s", entity, from, to),
	}
}

// ValidationFailed returns a validation error.
func ValidationFailed(msg string) *Error {
	return &Error{Code: CodeValidationFailed, Message: msg}
}

// QuotaExceeded returns a quota exceeded error.
func QuotaExceeded(quotaType string, limit, requested int) *Error {
	return &Error{
		Code:    CodeQuotaExceeded,
		Message: fmt.Sprintf("quota %s exceeded: limit %d, requested %d", quotaType, limit, requested),
	}
}

// Duplicate returns a duplicate submission error.
func Duplicate(key string) *Error {
	return &Error{Code: CodeDuplicate, Message: fmt.Sprintf("duplicate submission: %s", key)}
}

// Unauthorized returns an authentication failure without exposing credentials.
func Unauthorized(msg string) *Error {
	return &Error{Code: CodeUnauthorized, Message: msg}
}

// Forbidden returns an authorization failure for an authenticated principal.
func Forbidden(msg string) *Error {
	return &Error{Code: CodeForbidden, Message: msg}
}

// AsCode extracts the Code from an error chain, defaulting to CodeInternal.
func AsCode(err error) Code {
	var ae *Error
	if errors.As(err, &ae) {
		return ae.Code
	}
	return CodeInternal
}

// IsCode returns true if err matches the given code.
func IsCode(err error, code Code) bool {
	var ae *Error
	if errors.As(err, &ae) {
		return ae.Code == code
	}
	return false
}

// IsNotFound returns true if err is a not-found error.
func IsNotFound(err error) bool { return IsCode(err, CodeNotFound) }

// IsConflict returns true if err is an optimistic-lock conflict.
func IsConflict(err error) bool { return IsCode(err, CodeConflict) }

// IsInvalidTransition returns true if err is an illegal state transition.
func IsInvalidTransition(err error) bool { return IsCode(err, CodeInvalidState) }

// IsQuotaExceeded returns true if err is a quota exceeded error.
func IsQuotaExceeded(err error) bool { return IsCode(err, CodeQuotaExceeded) }

// IsDuplicate returns true if err is a duplicate submission error.
func IsDuplicate(err error) bool { return IsCode(err, CodeDuplicate) }
