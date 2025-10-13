package sprouter

import (
	"context"
	"net/http"

	"github.com/julienschmidt/httprouter"
)

type Sprouter struct {
	*httprouter.Router
}

func New() *Sprouter {
	return &Sprouter{
		Router: httprouter.New(),
	}
}

type Handle func(context.Context, any) (any, error)

func (s *Sprouter) wrap(handle Handle) httprouter.Handle {
	return func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		ctx := req.Context()
		result, err := handle(ctx, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(result.(string)))
	}
}

// GET is a shortcut for router.Handle(http.MethodGet, path, handle)
func (s *Sprouter) GET(path string, handle Handle) {
	s.Router.Handle(http.MethodGet, path, s.wrap(handle))
}

// HEAD is a shortcut for router.Handle(http.MethodHead, path, handle)
func (s *Sprouter) HEAD(path string, handle Handle) {
	s.Router.Handle(http.MethodHead, path, s.wrap(handle))
}

// OPTIONS is a shortcut for router.Handle(http.MethodOptions, path, handle)
func (s *Sprouter) OPTIONS(path string, handle Handle) {
	s.Router.Handle(http.MethodOptions, path, s.wrap(handle))
}

// POST is a shortcut for router.Handle(http.MethodPost, path, handle)
func (s *Sprouter) POST(path string, handle Handle) {
	s.Router.Handle(http.MethodPost, path, s.wrap(handle))
}

// PUT is a shortcut for router.Handle(http.MethodPut, path, handle)
func (s *Sprouter) PUT(path string, handle Handle) {
	s.Router.Handle(http.MethodPut, path, s.wrap(handle))
}

// PATCH is a shortcut for router.Handle(http.MethodPatch, path, handle)
func (s *Sprouter) PATCH(path string, handle Handle) {
	s.Router.Handle(http.MethodPatch, path, s.wrap(handle))
}

// DELETE is a shortcut for router.Handle(http.MethodDelete, path, handle)
func (s *Sprouter) DELETE(path string, handle Handle) {
	s.Router.Handle(http.MethodDelete, path, s.wrap(handle))
}
