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

// InsufficientBalanceError is a movement that would leave a holding (or a debt) below zero.
type InsufficientBalanceError struct {
	Message string
}

func (e InsufficientBalanceError) Error() string {
	return e.Message
}

func NewInsufficientBalanceError(msg string) error {
	return InsufficientBalanceError{Message: msg}
}

// NotRevertibleError is a movement that can't be undone, and the message says why.
type NotRevertibleError struct {
	Message string
}

func (e NotRevertibleError) Error() string {
	return e.Message
}

func NewNotRevertibleError(msg string) error {
	return NotRevertibleError{Message: msg}
}

// TransactionsUnavailableError: the database can't run transactions (a standalone MongoDB, not a replica
// set), so a write that needs one wasn't attempted.
type TransactionsUnavailableError struct {
	Message string
}

func (e TransactionsUnavailableError) Error() string {
	return e.Message
}

func NewTransactionsUnavailableError(msg string) error {
	return TransactionsUnavailableError{Message: msg}
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
