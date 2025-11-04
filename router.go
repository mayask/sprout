package sprout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/go-playground/validator/v10"
	"github.com/julienschmidt/httprouter"
)

type Sprout struct {
	*httprouter.Router
	validate *validator.Validate
	config   *Config
	openapi  *openAPIDocument
	parent   *Sprout
	order    *orderSeq
	registry *routerRegistry

	mwMu        sync.RWMutex
	middlewares []middlewareLayer
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

	openapiInfo *OpenAPIInfo
}

// Option mutates router configuration before the Sprout instance is constructed.
type Option func(*Config)

// New creates a new Sprout router with default configuration
func New() *Sprout {
	return NewWithConfig(nil)
}

// NewWithConfig creates a new Sprout router with custom configuration
func NewWithConfig(config *Config, opts ...Option) *Sprout {
	if config == nil {
		config = &Config{}
	}

	for _, opt := range opts {
		if opt != nil {
			opt(config)
		}
	}

	// Default to strict error type checking
	if config.StrictErrorTypes == nil {
		defaultStrict := true
		config.StrictErrorTypes = &defaultStrict
	}

	registry := newRouterRegistry()

	validate := validator.New(validator.WithRequiredStructEnabled())

	s := &Sprout{
		Router:   httprouter.New(),
		validate: validate,
		config:   config,
		openapi:  newOpenAPIDocument(config.openapiInfo),
		order:    &orderSeq{},
		registry: registry,
	}
	registry.add(s)

	// Route 404 Not Found errors through ErrorHandler for consistent error handling
	s.Router.NotFound = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.dispatchFallback(w, r, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handleError(s, w, r, &Error{
				Kind:    ErrorKindNotFound,
				Message: fmt.Sprintf("route not found: %s %s", r.Method, r.URL.Path),
			})
		}))
	})

	// Route 405 Method Not Allowed errors through ErrorHandler for consistent error handling
	s.Router.MethodNotAllowed = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.dispatchFallback(w, r, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handleError(s, w, r, &Error{
				Kind:    ErrorKindMethodNotAllowed,
				Message: fmt.Sprintf("method not allowed: %s %s", r.Method, r.URL.Path),
			})
		}))
	})

	// Expose generated OpenAPI specification
	swaggerPath := joinPath(s.config.BasePath, "/swagger")
	s.Router.GET(swaggerPath, s.openapi.ServeHTTP)

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

// combineBasePath merges multiple path segments into a normalized base path.
func combineBasePath(paths ...string) string {
	var result string
	for _, segment := range paths {
		segment = strings.TrimSpace(segment)
		if segment == "" || segment == "/" {
			continue
		}
		result = joinPath(result, segment)
	}

	if result == "" || result == "/" {
		return ""
	}
	return strings.TrimSuffix(result, "/")
}

// handle is a helper that applies route config and registers a handler
func handle[Req, Resp any](s *Sprout, method, path string, h Handle[Req, Resp], opts ...RouteOption) {
	cfg := &routeConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// Prepend base path if configured
	fullPath := joinPath(s.config.BasePath, path)

	if s.openapi != nil {
		s.openapi.RegisterRoute(method, fullPath, typeOf[Req](), typeOf[Resp](), cfg.expectedErrors)
	}

	entry := &routeEntry{
		owner:           s,
		order:           s.order.Next(),
		routeMiddleware: cfg.middlewares,
	}
	entry.fn = wrap(entry, h, cfg)

	s.Router.Handle(method, fullPath, func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		entry.owner.dispatchRoute(w, req, ps, entry)
	})
}

// Mount creates a child router that shares the underlying router and validator.
// The child inherits configuration such as error handlers, while applying an additional base path prefix.
func (s *Sprout) Mount(prefix string, config *Config) *Sprout {
	var childConfig Config
	if config != nil {
		childConfig = *config
	}

	if childConfig.ErrorHandler == nil {
		childConfig.ErrorHandler = s.config.ErrorHandler
	}

	if childConfig.StrictErrorTypes == nil {
		strict := *s.config.StrictErrorTypes
		childConfig.StrictErrorTypes = &strict
	}

	if childConfig.openapiInfo == nil {
		childConfig.openapiInfo = s.config.openapiInfo
	}

	childConfig.BasePath = combineBasePath(s.config.BasePath, prefix, childConfig.BasePath)

	child := &Sprout{
		Router:   s.Router,
		validate: s.validate,
		config:   &childConfig,
		openapi:  s.openapi,
		parent:   s,
		order:    s.order,
		registry: s.registry,
	}
	s.registry.add(child)

	return child
}

// RegisterCustomTypeFunc exposes validator.RegisterCustomTypeFunc to allow custom type handling.
func (s *Sprout) RegisterCustomTypeFunc(fn validator.CustomTypeFunc, types ...any) {
	s.validate.RegisterCustomTypeFunc(fn, types...)
}

// RegisterValidation exposes validator.RegisterValidation to allow custom validation tags.
func (s *Sprout) RegisterValidation(tag string, fn validator.Func, callValidationEvenIfNull ...bool) error {
	return s.validate.RegisterValidation(tag, fn, callValidationEvenIfNull...)
}

// Use registers middleware that executes according to the router hierarchy.
func (s *Sprout) Use(mw Middleware) {
	if mw == nil {
		return
	}

	layer := middlewareLayer{
		order: s.order.Next(),
		fn:    mw,
	}

	s.mwMu.Lock()
	s.middlewares = append(s.middlewares, layer)
	s.mwMu.Unlock()
}

// RouteOption is a function that configures a route
type RouteOption func(*routeConfig)

// routeConfig holds configuration for a route
type routeConfig struct {
	expectedErrors []reflect.Type
	middlewares    []Middleware
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

// WithMiddleware attaches middleware that only runs for the specific route.
func WithMiddleware(mw ...Middleware) RouteOption {
	return func(cfg *routeConfig) {
		for _, fn := range mw {
			if fn == nil {
				continue
			}
			cfg.middlewares = append(cfg.middlewares, fn)
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

func wrap[Req, Resp any](entry *routeEntry, handle Handle[Req, Resp], cfg *routeConfig) Middleware {
	return func(w http.ResponseWriter, req *http.Request, next Next) {
		s := entry.owner
		ctx := req.Context()

		// Parse request into the typed DTO
		var reqDTO Req
		reqValue := reflect.ValueOf(&reqDTO).Elem()
		reqType := reqValue.Type()
		params := Params(req)

		// Iterate through struct fields and populate from different sources
		for i := 0; i < reqType.NumField(); i++ {
			field := reqType.Field(i)
			fieldValue := reqValue.Field(i)

			if !fieldValue.CanSet() {
				continue
			}

			// Handle path parameters
			if pathTag := field.Tag.Get("path"); pathTag != "" {
				paramValue := ""
				if params != nil {
					paramValue = params.ByName(pathTag)
				}
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
			if errors.Is(err, ErrNext) {
				next(nil)
				return
			}

			errType := reflect.TypeOf(err)
			if errType.Kind() == reflect.Ptr {
				errType = errType.Elem()
			}

			declared := false
			for _, expected := range cfg.expectedErrors {
				if errType == expected {
					declared = true
					break
				}
			}

			if declared {
				enforceValidation := true
				if s.config.StrictErrorTypes != nil && !*s.config.StrictErrorTypes {
					enforceValidation = false
				}

				if handled, fallbackErr := writeTypedErrorResponse(s, w, req, err, http.StatusInternalServerError, enforceValidation); handled {
					if fallbackErr != nil {
						handleError(s, w, req, fallbackErr)
					}
					return
				} else if fallbackErr != nil {
					handleError(s, w, req, fallbackErr)
					return
				}
			}

			if *s.config.StrictErrorTypes {
				handleError(s, w, req, &Error{
					Kind:    ErrorKindUndeclaredError,
					Message: fmt.Sprintf("handler returned undeclared error type: %T", err),
					Err:     err,
				})
				return
			}
			handleError(s, w, req, err)
			return
		}

		// Handle nil response by creating empty instance
		if respDTO == nil {
			respDTO = new(Resp)
		}

		// Validate response DTO
		if err := s.validate.Struct(respDTO); err != nil {
			handleError(s, w, req, &Error{
				Kind:    ErrorKindResponseValidation,
				Message: "response validation failed",
				Err:     err,
			})
			return
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
		if !shouldWriteBody(req.Method, statusCode) {
			return
		}
		payload := prepareResponseBody(respDTO)
		if encodeErr := json.NewEncoder(w).Encode(payload); encodeErr != nil {
			// Note: headers already written, so handleError can't change the status code
			handleError(s, w, req, &Error{
				Kind:    ErrorKindSerialization,
				Message: "failed to encode response",
				Err:     encodeErr,
			})
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

func writeTypedErrorResponse(s *Sprout, w http.ResponseWriter, req *http.Request, err error, defaultStatus int, enforceValidation bool) (bool, error) {
	if err == nil {
		return false, nil
	}

	var sproutErr *Error
	if errors.As(err, &sproutErr) {
		return false, nil
	}

	errValue := reflect.ValueOf(err)
	if !isStructLike(errValue) {
		return false, nil
	}

	if enforceValidation {
		if validationErr := s.validate.Struct(err); validationErr != nil {
			return false, &Error{
				Kind:    ErrorKindErrorValidation,
				Message: "error response validation failed",
				Err:     validationErr,
			}
		}
	}

	statusCode := extractStatusCode(reflect.TypeOf(err), defaultStatus)
	customHeaders := extractHeaders(reflect.ValueOf(err))
	for name, value := range customHeaders {
		w.Header().Set(name, value)
	}

	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}

	w.WriteHeader(statusCode)
	if !shouldWriteBody(req.Method, statusCode) {
		return true, nil
	}

	if encodeErr := json.NewEncoder(w).Encode(toJSONMap(err)); encodeErr != nil {
		return false, &Error{
			Kind:    ErrorKindSerialization,
			Message: "failed to encode error response",
			Err:     encodeErr,
		}
	}

	return true, nil
}

func isStructLike(v reflect.Value) bool {
	if !v.IsValid() {
		return false
	}

	t := v.Type()
	if t.Kind() == reflect.Ptr {
		if v.IsNil() {
			return false
		}
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return false
	}

	n := t.NumField()
	if n == 0 {
		return true
	}

	for i := 0; i < n; i++ {
		field := t.Field(i)
		if field.IsExported() {
			return true
		}
		if field.Tag.Get("http") != "" || field.Tag.Get("json") != "" {
			return true
		}
	}

	return false
}

// shouldWriteBody determines whether a response body is allowed for the given method/status combination.
func shouldWriteBody(method string, status int) bool {
	if method == http.MethodHead {
		return false
	}

	if status >= 100 && status < 200 {
		return false
	}

	switch status {
	case http.StatusNoContent, http.StatusResetContent, http.StatusNotModified:
		return false
	}

	return true
}

func prepareResponseBody(resp any) any {
	if resp == nil {
		return nil
	}
	if unwrapped, ok := unwrapJSONFieldValue(reflect.ValueOf(resp)); ok {
		return unwrapped
	}
	if isStructLike(reflect.ValueOf(resp)) {
		return toJSONMap(resp)
	}
	return resp
}
