package errors

import "fmt"

type ResourceNotFoundError struct {
	Message string
}

func (e ResourceNotFoundError) Error() string {
	return e.Message
}

func NewResourceNotFoundError(msg string) error {
	return ResourceNotFoundError{Message: msg}
}

type DuplicateResourceError struct {
	Message string
}

func (e DuplicateResourceError) Error() string {
	return e.Message
}

func NewDuplicateResourceError(msg string) error {
	return DuplicateResourceError{Message: msg}
}

type ResourceInUseError struct {
	Message string
}

func (e ResourceInUseError) Error() string {
	return e.Message
}

func NewResourceInUseError(msg string) error {
	return ResourceInUseError{Message: msg}
}

// LimitExceededError is a per-user quota hit: one account can't grow without bound and starve the others.
type LimitExceededError struct {
	Message string
}

func (e LimitExceededError) Error() string {
	return e.Message
}

func NewLimitExceededError(msg string) error {
	return LimitExceededError{Message: msg}
}

type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

type ValidationErrors struct {
	Errors []ValidationError
}

func (e ValidationErrors) Error() string {
	return fmt.Sprintf("validation failed with %d errors", len(e.Errors))
}

func NewValidationErrors(errs []ValidationError) error {
	return ValidationErrors{Errors: errs}
}
