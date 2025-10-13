package sprout

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

func wrap[Req, Resp any](s *Sprout, handle Handle[Req, Resp]) httprouter.Handle {
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

// GET is a shortcut for router.Handle(http.MethodGet, path, handle)
func GET[Req, Resp any](s *Sprout, path string, handle Handle[Req, Resp]) {
	s.Router.Handle(http.MethodGet, path, wrap(s, handle))
}

// HEAD is a shortcut for router.Handle(http.MethodHead, path, handle)
func HEAD[Req, Resp any](s *Sprout, path string, handle Handle[Req, Resp]) {
	s.Router.Handle(http.MethodHead, path, wrap(s, handle))
}

// OPTIONS is a shortcut for router.Handle(http.MethodOptions, path, handle)
func OPTIONS[Req, Resp any](s *Sprout, path string, handle Handle[Req, Resp]) {
	s.Router.Handle(http.MethodOptions, path, wrap(s, handle))
}

// POST is a shortcut for router.Handle(http.MethodPost, path, handle)
func POST[Req, Resp any](s *Sprout, path string, handle Handle[Req, Resp]) {
	s.Router.Handle(http.MethodPost, path, wrap(s, handle))
}

// PUT is a shortcut for router.Handle(http.MethodPut, path, handle)
func PUT[Req, Resp any](s *Sprout, path string, handle Handle[Req, Resp]) {
	s.Router.Handle(http.MethodPut, path, wrap(s, handle))
}

// PATCH is a shortcut for router.Handle(http.MethodPatch, path, handle)
func PATCH[Req, Resp any](s *Sprout, path string, handle Handle[Req, Resp]) {
	s.Router.Handle(http.MethodPatch, path, wrap(s, handle))
}

// DELETE is a shortcut for router.Handle(http.MethodDelete, path, handle)
func DELETE[Req, Resp any](s *Sprout, path string, handle Handle[Req, Resp]) {
	s.Router.Handle(http.MethodDelete, path, wrap(s, handle))
}
