package sprout

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
)

// ErrorKind represents the category of error that occurred during request processing.
type ErrorKind string

const (
	// ErrorKindParse indicates a failure to parse request parameters (path, query, headers).
	// This typically occurs when type conversion fails (e.g., "abc" to int).
	ErrorKindParse ErrorKind = "parse_error"

	// ErrorKindValidation indicates request validation failed.
	// This occurs when the request doesn't satisfy validation constraints (e.g., required fields missing).
	ErrorKindValidation ErrorKind = "validation_error"

	// ErrorKindResponseValidation indicates response validation failed (internal error).
	// This occurs when the handler returns a response that doesn't satisfy validation constraints.
	ErrorKindResponseValidation ErrorKind = "response_validation_error"

	// ErrorKindErrorValidation indicates error response validation failed (internal error).
	// This occurs when a typed error doesn't satisfy its validation constraints.
	ErrorKindErrorValidation ErrorKind = "error_validation_error"

	// ErrorKindUndeclaredError indicates a handler returned an undeclared error type (internal error).
	// This occurs when StrictErrorTypes is enabled and a handler returns an error type not listed in WithErrors().
	ErrorKindUndeclaredError ErrorKind = "undeclared_error_type"

	// ErrorKindNotFound indicates no route matched the request.
	// This occurs when the requested path doesn't match any registered routes.
	ErrorKindNotFound ErrorKind = "not_found"

	// ErrorKindMethodNotAllowed indicates the HTTP method is not allowed for the requested route.
	// This occurs when a route exists but doesn't support the requested HTTP method.
	ErrorKindMethodNotAllowed ErrorKind = "method_not_allowed"

	// ErrorKindSerialization indicates JSON serialization failed (internal error).
	// This occurs when encoding a response or error to JSON fails.
	ErrorKindSerialization ErrorKind = "serialization_error"
)

// Error represents an error from Sprout's request processing pipeline.
// It provides context about what went wrong and where in the processing pipeline the error occurred.
type Error struct {
	Kind    ErrorKind // Category of error
	Message string    // Human-readable message
	Err     error     // Underlying error (can be nil)

	// Fields lists request field failures in request terms for parse and
	// validation errors that can be attributed to fields. Err keeps the
	// original error (validator.ValidationErrors, TypeValidationErrors,
	// *ParseParameterError) for existing callers.
	Fields FieldErrors
}

// FieldError describes one request field failure using request names: the
// location the value came from and its request-relative path.
type FieldError struct {
	// Location is where the field was sent: body, query, path, or header.
	Location ParameterSource
	// Field is the request-relative path: JSON names inside bodies
	// (grant_type, items[0].name), the form key for form bodies, and the
	// parameter name for query/path/header values (wallet_ids[0], id,
	// X-Correlation-ID). Empty when the failure concerns a whole body.
	Field string
	// Tag is the failed validate tag (required, oneof, ...), "type" for type
	// validators, or "decode" when the value could not be converted.
	Tag string
	// Param is the validate tag parameter, e.g. "a b" for oneof=a b.
	Param string
	// Kind is the kind of the field's Go type, when known.
	Kind reflect.Kind
	// Value is the offending value: the decoded value for validation
	// failures, the raw input for decode failures.
	Value any
	// Err is the underlying error for type validator and decode failures.
	Err error
}

func (e FieldError) Error() string {
	field := e.Field
	if field == "" {
		field = string(e.Location)
	}
	switch {
	case e.Err != nil:
		return fmt.Sprintf("%s %s: %v", e.Location, field, e.Err)
	case e.Param != "":
		return fmt.Sprintf("%s %s failed on %s=%s", e.Location, field, e.Tag, e.Param)
	default:
		return fmt.Sprintf("%s %s failed on %s", e.Location, field, e.Tag)
	}
}

// FieldErrors is a list of request field failures.
type FieldErrors []FieldError

func (e FieldErrors) Error() string {
	messages := make([]string, len(e))
	for i, fieldErr := range e {
		messages[i] = fieldErr.Error()
	}
	return strings.Join(messages, "; ")
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

// Unwrap returns the underlying error, allowing error chain traversal.
func (e *Error) Unwrap() error {
	return e.Err
}

// ParameterSource indicates where a parameter came from in the HTTP request.
type ParameterSource string

const (
	ParameterSourcePath   ParameterSource = "path"
	ParameterSourceQuery  ParameterSource = "query"
	ParameterSourceHeader ParameterSource = "header"
	ParameterSourceBody   ParameterSource = "body"
)

// ParseParameterError represents an error parsing a path, query, or header parameter.
// This provides structured information similar to json.UnmarshalTypeError.
type ParseParameterError struct {
	// Parameter is the name of the parameter that failed to parse (e.g., "page", "id").
	Parameter string

	// Source indicates where the parameter came from (path, query, or header).
	Source ParameterSource

	// Value is the raw string value that failed to parse.
	Value string

	// Err is the underlying parse error (e.g., from strconv).
	Err error
}

// Error implements the error interface.
func (e *ParseParameterError) Error() string {
	return fmt.Sprintf("failed to parse %s parameter '%s': %v", e.Source, e.Parameter, e.Err)
}

// Unwrap returns the underlying error.
func (e *ParseParameterError) Unwrap() error {
	return e.Err
}

// handleError routes errors to either the custom error handler or the default handler.
func handleError(s *Sprout, w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}

	normalizedErr := normalizeError(s, err)

	if s.config.ErrorHandler != nil {
		s.config.ErrorHandler(w, r, normalizedErr)
		return
	}

	if handled, fallbackErr := writeTypedErrorResponse(s, w, r, normalizedErr, http.StatusInternalServerError, true); handled {
		return
	} else if fallbackErr != nil {
		handleError(s, w, r, fallbackErr)
		return
	}

	var sproutErr *Error
	if errors.As(normalizedErr, &sproutErr) {
		switch sproutErr.Kind {
		case ErrorKindParse, ErrorKindValidation:
			http.Error(w, sproutErr.Error(), http.StatusBadRequest)
		case ErrorKindNotFound:
			http.Error(w, sproutErr.Error(), http.StatusNotFound)
		case ErrorKindMethodNotAllowed:
			http.Error(w, sproutErr.Error(), http.StatusMethodNotAllowed)
		case ErrorKindResponseValidation, ErrorKindErrorValidation, ErrorKindUndeclaredError, ErrorKindSerialization:
			http.Error(w, sproutErr.Error(), http.StatusInternalServerError)
		default:
			http.Error(w, sproutErr.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Fallback for non-Sprout errors (shouldn't normally happen)
	http.Error(w, normalizedErr.Error(), http.StatusInternalServerError)
}

func normalizeError(s *Sprout, err error) error {
	if s == nil || s.validate == nil {
		return err
	}

	var sproutErr *Error
	if errors.As(err, &sproutErr) {
		return err
	}

	if isStructLike(reflect.ValueOf(err)) {
		if s.config.StrictErrorTypes != nil && !*s.config.StrictErrorTypes {
			return err
		}
		if validationErr := s.validate.Struct(err); validationErr != nil {
			return &Error{
				Kind:    ErrorKindErrorValidation,
				Message: "error response validation failed",
				Err:     validationErr,
			}
		}
	}

	return err
}
