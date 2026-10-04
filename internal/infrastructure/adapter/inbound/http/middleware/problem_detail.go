package middleware

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	appErrors "github.com/GM-Tomas/base_project_go/internal/errors"
)

const (
	ProblemBaseURI    = "https://base.wealth/errors"
	ProblemJSONHeader = "application/problem+json"
)

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ProblemDetail struct {
	Type     string       `json:"type"`
	Title    string       `json:"title"`
	Status   int          `json:"status"`
	Detail   string       `json:"detail"`
	Instance string       `json:"instance,omitempty"`
	TraceID  string       `json:"traceId,omitempty"`
	Errors   []FieldError `json:"errors,omitempty"`
}

func WriteProblem(w http.ResponseWriter, r *http.Request, status int, slug string, title string, detail string, fieldErrors []FieldError) {
	traceID := GetTraceID(r.Context())
	if title == "" {
		title = http.StatusText(status)
	}

	prob := ProblemDetail{
		Type:     ProblemBaseURI + "/" + slug,
		Title:    title,
		Status:   status,
		Detail:   detail,
		Instance: r.URL.Path,
		TraceID:  traceID,
		Errors:   fieldErrors,
	}

	w.Header().Set("Content-Type", ProblemJSONHeader)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(prob)
}

func HandleError(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}

	switch e := err.(type) {
	case appErrors.ResourceNotFoundError:
		WriteProblem(w, r, http.StatusNotFound, "not-found", "Not Found", e.Message, nil)
	case appErrors.DuplicateResourceError:
		WriteProblem(w, r, http.StatusConflict, "conflict", "Conflict", e.Message, nil)
	case appErrors.ResourceInUseError:
		WriteProblem(w, r, http.StatusConflict, "conflict", "Conflict", e.Message, nil)
	case appErrors.LimitExceededError:
		WriteProblem(w, r, http.StatusConflict, "limit-exceeded", "Conflict", e.Message, nil)
	case appErrors.ValidationErrors:
		fieldErrs := make([]FieldError, len(e.Errors))
		messages := make([]string, len(e.Errors))
		for i, fe := range e.Errors {
			fieldErrs[i] = FieldError{Field: fe.Field, Message: fe.Message}
			messages[i] = fe.Message
		}
		// detail carries the messages too: the frontend shows detail, not the per-field list.
		WriteProblem(w, r, http.StatusBadRequest, "validation", "Bad Request", strings.Join(messages, "; "), fieldErrs)
	default:
		if isDomainValidationError(err) {
			WriteProblem(w, r, http.StatusBadRequest, "bad-request", "Bad Request", err.Error(), nil)
			return
		}
		// Infrastructure failure (DB unreachable, etc.): log it, never leak it to the client.
		log.Printf("unhandled error [traceId=%s] %s %s: %v", GetTraceID(r.Context()), r.Method, r.URL.Path, err)
		WriteInternalServerError(w, r)
	}
}

func isDomainValidationError(err error) bool {
	for _, target := range []error{
		model.ErrBlankLabel,
		model.ErrLabelTooLong,
		model.ErrNegativeMoney,
		model.ErrNonFiniteMoney,
		model.ErrInvalidUUID,
		model.ErrYearsOutOfRange,
		model.ErrYieldOutOfRange,
		model.ErrTooManyMilestones,
	} {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

func WriteInternalServerError(w http.ResponseWriter, r *http.Request) {
	WriteProblem(w, r, http.StatusInternalServerError, "internal", "Internal Server Error", "An unexpected error occurred. Quote the traceId when reporting it.", nil)
}

func WriteUnauthorized(w http.ResponseWriter, r *http.Request, detail string) {
	if detail == "" {
		detail = "Missing or invalid access token"
	}
	WriteProblem(w, r, http.StatusUnauthorized, "unauthorized", "Unauthorized", detail, nil)
}
