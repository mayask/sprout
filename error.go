package sprout

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
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
