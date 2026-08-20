package domain

import (
	"errors"
	"fmt"
)

// Common sentinel errors for the domain layer.
var (
	ErrNotFound      = errors.New("entity not found")
	ErrConflict      = errors.New("optimistic lock conflict")
	ErrInvalidState  = errors.New("invalid state transition")
	ErrQuotaExceeded = errors.New("quota exceeded")
	ErrDuplicate     = errors.New("duplicate")
	ErrLeaseExpired  = errors.New("lease expired")
	ErrPreempted     = errors.New("preempted")
)

// NotFoundError wraps entity and id for richer not-found reporting.
type NotFoundError struct {
	Entity string
	ID     string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s not found: %s", e.Entity, e.ID)
}

func (e *NotFoundError) Unwrap() error { return ErrNotFound }

// NewNotFoundError creates a NotFoundError.
func NewNotFoundError(entity, id string) *NotFoundError {
	return &NotFoundError{Entity: entity, ID: id}
}

// IsNotFound returns true if err is a domain not-found error.
// The persistence layer is responsible for translating storage-level
// "no rows" signals into domain NotFoundError values, so the domain
// layer never depends on a storage implementation.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrNotFound) {
		return true
	}
	var nfe *NotFoundError
	return errors.As(err, &nfe)
}

// ConflictError indicates an optimistic-lock version mismatch.
type ConflictError struct {
	Entity  string
	ID      string
	Version int
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s %s version conflict at %d", e.Entity, e.ID, e.Version)
}

func (e *ConflictError) Unwrap() error { return ErrConflict }

// NewConflictError creates a ConflictError.
func NewConflictError(entity, id string, version int) *ConflictError {
	return &ConflictError{Entity: entity, ID: id, Version: version}
}

// IsConflict returns true if err wraps ErrConflict.
func IsConflict(err error) bool {
	return errors.Is(err, ErrConflict)
}
