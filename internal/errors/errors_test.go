package errors_test

import (
	"testing"

	appErrors "github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/stretchr/testify/assert"
)

func TestErrorMessages(t *testing.T) {
	assert.EqualError(t, appErrors.NewResourceNotFoundError("not found"), "not found")
	assert.EqualError(t, appErrors.NewDuplicateResourceError("dup"), "dup")
	assert.EqualError(t, appErrors.NewResourceInUseError("in use"), "in use")
	assert.EqualError(t, appErrors.NewLimitExceededError("too many"), "too many")
	assert.EqualError(t, appErrors.ValidationError{Field: "name", Message: "required"}, "name: required")
	assert.EqualError(t, appErrors.NewValidationErrors([]appErrors.ValidationError{{}, {}}), "validation failed with 2 errors")
}
