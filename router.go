package sprouter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"

	"github.com/go-playground/validator/v10"
	"github.com/julienschmidt/httprouter"
)

type Sprouter struct {
	*httprouter.Router
	validate *validator.Validate
}

func New() *Sprouter {
	return &Sprouter{
		Router:   httprouter.New(),
		validate: validator.New(),
	}
}

type Handle[Req, Resp any] func(context.Context, *Req) (*Resp, error)

func wrap[Req, Resp any](s *Sprouter, handle Handle[Req, Resp]) httprouter.Handle {
	return func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		ctx := req.Context()

		// Parse request body into the typed DTO
		var reqDTO Req
		reqType := reflect.TypeOf(reqDTO)

		// Only parse body if it's not an empty struct
		if req.Body != nil && reqType.Kind() == reflect.Struct && reqType.NumField() > 0 {
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

				// Validate request DTO
				if err := s.validate.Struct(reqDTO); err != nil {
					http.Error(w, "Request validation failed: "+err.Error(), http.StatusBadRequest)
					return
				}
			}
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
func GET[Req, Resp any](s *Sprouter, path string, handle Handle[Req, Resp]) {
	s.Router.Handle(http.MethodGet, path, wrap(s, handle))
}

// HEAD is a shortcut for router.Handle(http.MethodHead, path, handle)
func HEAD[Req, Resp any](s *Sprouter, path string, handle Handle[Req, Resp]) {
	s.Router.Handle(http.MethodHead, path, wrap(s, handle))
}

// OPTIONS is a shortcut for router.Handle(http.MethodOptions, path, handle)
func OPTIONS[Req, Resp any](s *Sprouter, path string, handle Handle[Req, Resp]) {
	s.Router.Handle(http.MethodOptions, path, wrap(s, handle))
}

// POST is a shortcut for router.Handle(http.MethodPost, path, handle)
func POST[Req, Resp any](s *Sprouter, path string, handle Handle[Req, Resp]) {
	s.Router.Handle(http.MethodPost, path, wrap(s, handle))
}

// PUT is a shortcut for router.Handle(http.MethodPut, path, handle)
func PUT[Req, Resp any](s *Sprouter, path string, handle Handle[Req, Resp]) {
	s.Router.Handle(http.MethodPut, path, wrap(s, handle))
}

// PATCH is a shortcut for router.Handle(http.MethodPatch, path, handle)
func PATCH[Req, Resp any](s *Sprouter, path string, handle Handle[Req, Resp]) {
	s.Router.Handle(http.MethodPatch, path, wrap(s, handle))
}

// DELETE is a shortcut for router.Handle(http.MethodDelete, path, handle)
func DELETE[Req, Resp any](s *Sprouter, path string, handle Handle[Req, Resp]) {
	s.Router.Handle(http.MethodDelete, path, wrap(s, handle))
}
