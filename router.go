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

	"github.com/go-playground/validator/v10"
	"github.com/julienschmidt/httprouter"
)

type Sprout struct {
	*httprouter.Router
	validate *validator.Validate
}

func New() *Sprout {
	return &Sprout{
		Router:   httprouter.New(),
		validate: validator.New(),
	}
}

type Handle[Req, Resp any] func(context.Context, *Req) (*Resp, error)

// handle is a helper that applies route config and registers a handler
func handle[Req, Resp any](s *Sprout, method, path string, h Handle[Req, Resp], opts ...RouteOption) {
	cfg := &routeConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	s.Router.Handle(method, path, wrap(s, h, cfg))
}

// HTTPError is an interface for errors that can provide HTTP-specific information
type HTTPError interface {
	error
	StatusCode() int
	ResponseBody() interface{}
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
					http.Error(w, fmt.Sprintf("Invalid path parameter '%s': %v", pathTag, err), http.StatusBadRequest)
					return
				}
			}

			// Handle query parameters
			if queryTag := field.Tag.Get("query"); queryTag != "" {
				queryValue := req.URL.Query().Get(queryTag)
				if err := setFieldValue(fieldValue, queryValue); err != nil {
					http.Error(w, fmt.Sprintf("Invalid query parameter '%s': %v", queryTag, err), http.StatusBadRequest)
					return
				}
			}

			// Handle headers
			if headerTag := field.Tag.Get("header"); headerTag != "" {
				headerValue := req.Header.Get(headerTag)
				if err := setFieldValue(fieldValue, headerValue); err != nil {
					http.Error(w, fmt.Sprintf("Invalid header '%s': %v", headerTag, err), http.StatusBadRequest)
					return
				}
			}
		}

		// Parse JSON body into struct (excluding tagged fields)
		if req.Body != nil && req.ContentLength > 0 {
			body, err := io.ReadAll(req.Body)
			if err != nil {
				http.Error(w, "Failed to read request body", http.StatusBadRequest)
				return
			}
			defer req.Body.Close()

			if len(body) > 0 {
				if err := json.Unmarshal(body, &reqDTO); err != nil {
					http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
					return
				}
			}
		}

		// Validate request DTO
		if err := s.validate.Struct(reqDTO); err != nil {
			http.Error(w, "Request validation failed: "+err.Error(), http.StatusBadRequest)
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
					log.Printf("WARNING: handler returned unexpected error type: %T (expected one of: %v)", err, cfg.expectedErrors)
				}
			}

			// Try to handle error using HTTPError interface
			if httpErr, ok := err.(HTTPError); ok {
				errorBody := httpErr.ResponseBody()

				// Validate error response body
				if errorBody != nil {
					if err := s.validate.Struct(errorBody); err != nil {
						log.Printf("ERROR: error response validation failed: %v", err)
						http.Error(w, "Error response validation failed: "+err.Error(), http.StatusInternalServerError)
						return
					}
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(httpErr.StatusCode())
				if err := json.NewEncoder(w).Encode(errorBody); err != nil {
					http.Error(w, "Failed to encode error response", http.StatusInternalServerError)
				}
				return
			}

			// Fallback to generic 500 error
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Validate response DTO
		if respDTO != nil {
			if err := s.validate.Struct(respDTO); err != nil {
				http.Error(w, "Response validation failed: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}

		// Serialize response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(respDTO); err != nil {
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
