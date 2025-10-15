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
// Status codes are defined using struct tags: `http:"status=404"`
// The error struct itself is serialized as the response body
type HTTPError interface {
	error
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

// extractStatusCode reads the HTTP status code from struct tags
// Looks for a field with `http:"status=XXX"` tag
// defaultCode is returned if no status tag is found
func extractStatusCode(t reflect.Type, defaultCode int) int {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if httpTag := field.Tag.Get("http"); httpTag != "" {
			// Parse "status=404" or "status=404,description=..."
			parts := strings.Split(httpTag, ",")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if strings.HasPrefix(part, "status=") {
					statusStr := strings.TrimPrefix(part, "status=")
					if code, err := strconv.Atoi(statusStr); err == nil {
						return code
					}
				}
			}
		}
	}
	return defaultCode
}

// extractHeaders reads HTTP headers from named fields with `header:` tags
// Takes a reflect.Value (not Type) to read field values
// Returns a map of header names to values
func extractHeaders(v reflect.Value) map[string]string {
	headers := make(map[string]string)

	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fieldValue := v.Field(i)

		// Look for `header:"Header-Name"` tag
		if headerTag := field.Tag.Get("header"); headerTag != "" {
			// Get the string value of the field
			if fieldValue.Kind() == reflect.String {
				value := fieldValue.String()
				if value != "" {
					headers[headerTag] = value
				}
			}
		}
	}

	return headers
}

// shouldExcludeFromJSON checks if a field should be excluded from JSON serialization
// Fields with path, query, header, or http tags are excluded
func shouldExcludeFromJSON(field reflect.StructField) bool {
	// Check if field has json:"-" tag explicitly
	if jsonTag := field.Tag.Get("json"); jsonTag == "-" {
		return true
	}

	// Exclude fields with routing/metadata tags
	if field.Tag.Get("path") != "" {
		return true
	}
	if field.Tag.Get("query") != "" {
		return true
	}
	if field.Tag.Get("header") != "" {
		return true
	}
	if field.Tag.Get("http") != "" {
		return true
	}

	return false
}

// toJSONMap converts a struct to a map, excluding top-level fields with routing tags
// Nested objects are included as-is (routing tags only matter at the top level)
func toJSONMap(v interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	typ := val.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		fieldValue := val.Field(i)

		// Skip fields that should be excluded (only checks top-level tags)
		if shouldExcludeFromJSON(field) {
			continue
		}

		// Get JSON field name from tag, or use struct field name
		jsonName := field.Name
		if jsonTag := field.Tag.Get("json"); jsonTag != "" && jsonTag != "-" {
			// Parse json tag (handle "name,omitempty" format)
			parts := strings.Split(jsonTag, ",")
			if parts[0] != "" {
				jsonName = parts[0]
			}
			// Check for omitempty
			if len(parts) > 1 && parts[1] == "omitempty" {
				// Skip zero values
				if fieldValue.IsZero() {
					continue
				}
			}
		}

		// Include the field value as-is (nested structs handled by json.Encoder)
		result[jsonName] = fieldValue.Interface()
	}

	return result
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
			if _, ok := err.(HTTPError); ok {
				// Validate error response body
				if err := s.validate.Struct(err); err != nil {
					log.Printf("ERROR: error response validation failed: %v", err)
					http.Error(w, "Error response validation failed: "+err.Error(), http.StatusInternalServerError)
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
