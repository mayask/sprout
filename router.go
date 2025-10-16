package sprout

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/julienschmidt/httprouter"
)

type Sprout struct {
	*httprouter.Router
	validate *validator.Validate
	config   *Config
}

// Config holds configuration options for customizing Sprout's behavior.
type Config struct {
	// ErrorHandler is called when Sprout encounters system errors (parse, validation, etc.).
	// If nil, Sprout uses default error handling with appropriate HTTP status codes.
	//
	// The error parameter will be of type *Error, which can be extracted using errors.As().
	// This provides access to ErrorKind for categorizing errors and returning custom responses.
	ErrorHandler func(w http.ResponseWriter, r *http.Request, err error)

	// StrictErrorTypes controls whether handlers must declare error types via WithErrors().
	// When true (default), returning an undeclared error type results in 500 Internal Server Error.
	// When false, undeclared errors are logged as warnings but still processed.
	//
	// This encourages explicit error type declarations for better API documentation.
	StrictErrorTypes *bool

	// BasePath is a prefix prepended to all route paths registered with this router.
	// For example, with BasePath="/api/v1", a route registered as "/users" becomes "/api/v1/users".
	// Leading and trailing slashes are handled automatically.
	BasePath string
}

// New creates a new Sprout router with default configuration
func New() *Sprout {
	return NewWithConfig(nil)
}

// NewWithConfig creates a new Sprout router with custom configuration
func NewWithConfig(config *Config) *Sprout {
	if config == nil {
		config = &Config{}
	}
	// Default to strict error type checking
	if config.StrictErrorTypes == nil {
		defaultStrict := true
		config.StrictErrorTypes = &defaultStrict
	}

	s := &Sprout{
		Router:   httprouter.New(),
		validate: validator.New(),
		config:   config,
	}

	// Route 404 Not Found errors through ErrorHandler for consistent error handling
	s.Router.NotFound = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleError(s, w, r, &Error{
			Kind:    ErrorKindNotFound,
			Message: fmt.Sprintf("route not found: %s %s", r.Method, r.URL.Path),
		})
	})

	// Route 405 Method Not Allowed errors through ErrorHandler for consistent error handling
	s.Router.MethodNotAllowed = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleError(s, w, r, &Error{
			Kind:    ErrorKindMethodNotAllowed,
			Message: fmt.Sprintf("method not allowed: %s %s", r.Method, r.URL.Path),
		})
	})

	return s
}

type Handle[Req, Resp any] func(context.Context, *Req) (*Resp, error)

// joinPath joins base path and route path, handling slashes correctly
func joinPath(basePath, routePath string) string {
	// Clean up base path
	basePath = strings.TrimSpace(basePath)
	if basePath == "" {
		return routePath
	}

	// Ensure base path starts with / and doesn't end with /
	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}
	basePath = strings.TrimSuffix(basePath, "/")

	// Ensure route path starts with /
	if !strings.HasPrefix(routePath, "/") {
		routePath = "/" + routePath
	}

	return basePath + routePath
}

// handle is a helper that applies route config and registers a handler
func handle[Req, Resp any](s *Sprout, method, path string, h Handle[Req, Resp], opts ...RouteOption) {
	cfg := &routeConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// Prepend base path if configured
	fullPath := joinPath(s.config.BasePath, path)

	s.Router.Handle(method, fullPath, wrap(s, h, cfg))
}

// RouteOption is a function that configures a route
type RouteOption func(*routeConfig)

// routeConfig holds configuration for a route
type routeConfig struct {
	expectedErrors []reflect.Type
}

// WithErrors registers expected error types for validation and documentation
func WithErrors(errs ...error) RouteOption {
	return func(cfg *routeConfig) {
		for _, err := range errs {
			errType := reflect.TypeOf(err)
			// Handle both pointer and value types
			if errType.Kind() == reflect.Ptr {
				errType = errType.Elem()
			}
			cfg.expectedErrors = append(cfg.expectedErrors, errType)
		}
	}
}

// setFieldValue sets a reflect.Value from a string value, handling type conversion
func setFieldValue(fieldValue reflect.Value, value string) error {
	if value == "" {
		return nil // Skip empty values
	}

	switch fieldValue.Kind() {
	case reflect.String:
		fieldValue.SetString(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		intVal, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("failed to parse int: %w", err)
		}
		fieldValue.SetInt(intVal)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		uintVal, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return fmt.Errorf("failed to parse uint: %w", err)
		}
		fieldValue.SetUint(uintVal)
	case reflect.Float32, reflect.Float64:
		floatVal, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("failed to parse float: %w", err)
		}
		fieldValue.SetFloat(floatVal)
	case reflect.Bool:
		boolVal, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("failed to parse bool: %w", err)
		}
		fieldValue.SetBool(boolVal)
	default:
		return fmt.Errorf("unsupported field type: %s", fieldValue.Kind())
	}

	return nil
}

func wrap[Req, Resp any](s *Sprout, handle Handle[Req, Resp], cfg *routeConfig) httprouter.Handle {
	return func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		ctx := req.Context()

		// Parse request into the typed DTO
		var reqDTO Req
		reqValue := reflect.ValueOf(&reqDTO).Elem()
		reqType := reqValue.Type()

		// Iterate through struct fields and populate from different sources
		for i := 0; i < reqType.NumField(); i++ {
			field := reqType.Field(i)
			fieldValue := reqValue.Field(i)

			if !fieldValue.CanSet() {
				continue
			}

			// Handle path parameters
			if pathTag := field.Tag.Get("path"); pathTag != "" {
				paramValue := ps.ByName(pathTag)
				if err := setFieldValue(fieldValue, paramValue); err != nil {
					handleError(s, w, req, &Error{
						Kind:    ErrorKindParse,
						Message: fmt.Sprintf("invalid path parameter '%s'", pathTag),
						Err:     err,
					})
					return
				}
			}

			// Handle query parameters
			if queryTag := field.Tag.Get("query"); queryTag != "" {
				queryValue := req.URL.Query().Get(queryTag)
				if err := setFieldValue(fieldValue, queryValue); err != nil {
					handleError(s, w, req, &Error{
						Kind:    ErrorKindParse,
						Message: fmt.Sprintf("invalid query parameter '%s'", queryTag),
						Err:     err,
					})
					return
				}
			}

			// Handle headers
			if headerTag := field.Tag.Get("header"); headerTag != "" {
				headerValue := req.Header.Get(headerTag)
				if err := setFieldValue(fieldValue, headerValue); err != nil {
					handleError(s, w, req, &Error{
						Kind:    ErrorKindParse,
						Message: fmt.Sprintf("invalid header '%s'", headerTag),
						Err:     err,
					})
					return
				}
			}
		}

		// Parse JSON body into struct (excluding tagged fields)
		if req.Body != nil && req.ContentLength > 0 {
			body, err := io.ReadAll(req.Body)
			if err != nil {
				handleError(s, w, req, &Error{
					Kind:    ErrorKindParse,
					Message: "failed to read request body",
					Err:     err,
				})
				return
			}
			defer req.Body.Close()

			if len(body) > 0 {
				if err := json.Unmarshal(body, &reqDTO); err != nil {
					handleError(s, w, req, &Error{
						Kind:    ErrorKindParse,
						Message: "invalid JSON",
						Err:     err,
					})
					return
				}
			}
		}

		// Validate request DTO
		if err := s.validate.Struct(reqDTO); err != nil {
			handleError(s, w, req, &Error{
				Kind:    ErrorKindValidation,
				Message: "request validation failed",
				Err:     err,
			})
			return
		}

		// Call the handler
		respDTO, err := handle(ctx, &reqDTO)
		if err != nil {
			// Validate error type if expected errors are configured
			if len(cfg.expectedErrors) > 0 {
				errType := reflect.TypeOf(err)
				if errType.Kind() == reflect.Ptr {
					errType = errType.Elem()
				}

				found := false
				for _, expected := range cfg.expectedErrors {
					if errType == expected {
						found = true
						break
					}
				}

				if !found {
					// StrictErrorTypes is enabled by default
					if *s.config.StrictErrorTypes {
						log.Printf("ERROR: handler returned undeclared error type: %T (expected one of: %v)", err, cfg.expectedErrors)
						handleError(s, w, req, &Error{
							Kind:    ErrorKindUndeclaredError,
							Message: fmt.Sprintf("handler returned undeclared error type: %T", err),
							Err:     err,
						})
						return
					}
					log.Printf("WARNING: handler returned unexpected error type: %T (expected one of: %v)", err, cfg.expectedErrors)
				}
			}

			// Validate error response body
			if validationErr := s.validate.Struct(err); validationErr != nil {
				log.Printf("ERROR: error response validation failed: %v", validationErr)
				handleError(s, w, req, &Error{
					Kind:    ErrorKindErrorValidation,
					Message: "error response validation failed",
					Err:     validationErr,
				})
				return
			}

			// Extract status code and headers from struct tags (default to 500 for errors)
			errType := reflect.TypeOf(err)
			statusCode := extractStatusCode(errType, http.StatusInternalServerError)
			customHeaders := extractHeaders(reflect.ValueOf(err))

			// Set custom headers from struct tags
			for name, value := range customHeaders {
				w.Header().Set(name, value)
			}

			// Set Content-Type to application/json if not already set
			if w.Header().Get("Content-Type") == "" {
				w.Header().Set("Content-Type", "application/json")
			}
			w.WriteHeader(statusCode)
			// Convert to map to exclude routing/metadata fields from JSON
			if err := json.NewEncoder(w).Encode(toJSONMap(err)); err != nil {
				http.Error(w, "Failed to encode error response", http.StatusInternalServerError)
			}
			return
		}

		// Validate response DTO
		if respDTO != nil {
			if err := s.validate.Struct(respDTO); err != nil {
				handleError(s, w, req, &Error{
					Kind:    ErrorKindResponseValidation,
					Message: "response validation failed",
					Err:     err,
				})
				return
			}
		}

		// Extract status code and headers from response struct tags
		statusCode := http.StatusOK
		var customHeaders map[string]string
		if respDTO != nil {
			respType := reflect.TypeOf(respDTO)
			statusCode = extractStatusCode(respType, http.StatusOK)
			customHeaders = extractHeaders(reflect.ValueOf(respDTO))
		}

		// Set custom headers from struct tags
		for name, value := range customHeaders {
			w.Header().Set(name, value)
		}

		// Set Content-Type to application/json if not already set
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/json")
		}

		// Serialize response
		w.WriteHeader(statusCode)
		// Convert to map to exclude routing/metadata fields from JSON
		if err := json.NewEncoder(w).Encode(toJSONMap(respDTO)); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}
	}
}

// GET is a shortcut for handle(s, http.MethodGet, path, h, opts...)
func GET[Req, Resp any](s *Sprout, path string, h Handle[Req, Resp], opts ...RouteOption) {
	handle(s, http.MethodGet, path, h, opts...)
}

// HEAD is a shortcut for Handle(s, http.MethodHead, path, h, opts...)
func HEAD[Req, Resp any](s *Sprout, path string, h Handle[Req, Resp], opts ...RouteOption) {
	handle(s, http.MethodHead, path, h, opts...)
}

// OPTIONS is a shortcut for Handle(s, http.MethodOptions, path, h, opts...)
func OPTIONS[Req, Resp any](s *Sprout, path string, h Handle[Req, Resp], opts ...RouteOption) {
	handle(s, http.MethodOptions, path, h, opts...)
}

// POST is a shortcut for Handle(s, http.MethodPost, path, h, opts...)
func POST[Req, Resp any](s *Sprout, path string, h Handle[Req, Resp], opts ...RouteOption) {
	handle(s, http.MethodPost, path, h, opts...)
}

// PUT is a shortcut for Handle(s, http.MethodPut, path, h, opts...)
func PUT[Req, Resp any](s *Sprout, path string, h Handle[Req, Resp], opts ...RouteOption) {
	handle(s, http.MethodPut, path, h, opts...)
}

// PATCH is a shortcut for Handle(s, http.MethodPatch, path, h, opts...)
func PATCH[Req, Resp any](s *Sprout, path string, h Handle[Req, Resp], opts ...RouteOption) {
	handle(s, http.MethodPatch, path, h, opts...)
}

// DELETE is a shortcut for Handle(s, http.MethodDelete, path, h, opts...)
func DELETE[Req, Resp any](s *Sprout, path string, h Handle[Req, Resp], opts ...RouteOption) {
	handle(s, http.MethodDelete, path, h, opts...)
}
