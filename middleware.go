package sprout

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/julienschmidt/httprouter"
)

// Middleware allows intercepting requests before and after route handlers.
type Middleware func(http.ResponseWriter, *http.Request, Next)

// Next advances to the next middleware or handler in the chain.
// Pass a non-nil error to short-circuit the chain and trigger Sprout's error handling.
type Next func(error)

// ErrNext signals a typed handler should delegate to the next middleware.
var ErrNext = errors.New("sprout: next")

// middlewareLayer keeps the middleware function together with its registration
// order so we can sort and partition layers relative to routes.
type middlewareLayer struct {
	order int64
	fn    Middleware
}

// routeEntry wraps a typed handler with its parent router metadata and the
// order at which it was registered.
type routeEntry struct {
	owner           *Sprout
	order           int64
	fn              Middleware
	routeMiddleware []Middleware
}

// orderSeq provides a monotonic counter shared by routers so we can determine
// whether a middleware was registered before or after a given route.
type orderSeq struct {
	value atomic.Int64
}

func (o *orderSeq) Next() int64 {
	return o.value.Add(1)
}

// routerRegistry tracks all Sprout instances that share a backing httprouter so
// we can identify which middleware stacks apply to a request path.
type routerRegistry struct {
	mu      sync.RWMutex
	routers []*Sprout
}

func newRouterRegistry() *routerRegistry {
	return &routerRegistry{}
}

func (r *routerRegistry) add(s *Sprout) {
	r.mu.Lock()
	r.routers = append(r.routers, s)
	r.mu.Unlock()
}

func (r *routerRegistry) matchingRouters(path string) []*Sprout {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var matches []*Sprout
	// Collect routers whose BasePath prefixes the request path. This mimics the
	// express-style parent → child router lookup chain.
	for _, router := range r.routers {
		if router.matchesPath(path) {
			matches = append(matches, router)
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		return len(matches[i].config.BasePath) < len(matches[j].config.BasePath)
	})

	return matches
}

// dispatchRoute builds the middleware chain for a matched route and executes
// it, inserting the typed handler between middleware registered before and
// after the route.
func (s *Sprout) dispatchRoute(w http.ResponseWriter, req *http.Request, ps httprouter.Params, entry *routeEntry) {
	req = withParams(req, ps)

	before, after := gatherRouteMiddleware(entry)

	chain := make([]Middleware, 0, len(before)+len(entry.routeMiddleware)+1+len(after))
	chain = append(chain, before...)
	if len(entry.routeMiddleware) > 0 {
		chain = append(chain, entry.routeMiddleware...)
	}
	chain = append(chain, entry.fn)
	chain = append(chain, after...)

	runChain(chain, entry.owner, w, req)
}

// dispatchFallback runs the middleware chain for the path and then invokes the
// provided fallback handler (typically NotFound/MethodNotAllowed). This ensures
// pathless middleware can implement express-style fallbacks.
func (s *Sprout) dispatchFallback(w http.ResponseWriter, req *http.Request, fallback http.Handler) {
	req = withParams(req, nil)

	routers := s.registry.matchingRouters(req.URL.Path)
	layers := collectMiddlewareLayers(routers)

	chain := make([]Middleware, 0, len(layers)+1)
	for _, layer := range layers {
		chain = append(chain, layer.fn)
	}

	if fallback != nil {
		chain = append(chain, func(w http.ResponseWriter, r *http.Request, _ Next) {
			fallback.ServeHTTP(w, r)
		})
	}

	runChain(chain, s, w, req)
}

// gatherRouteMiddleware collates middleware from root → leaf routers and
// partitions them into layers that run before or after the route handler based
// on registration order.
func gatherRouteMiddleware(entry *routeEntry) (before []Middleware, after []Middleware) {
	routers := entry.owner.ancestorChain()
	layers := collectMiddlewareLayers(routers)

	for _, layer := range layers {
		if layer.order < entry.order {
			before = append(before, layer.fn)
		} else {
			after = append(after, layer.fn)
		}
	}
	return
}

// collectMiddlewareLayers aggregates middleware from a set of routers and sorts
// them by registration order to preserve express-like sequencing.
func collectMiddlewareLayers(routers []*Sprout) []middlewareLayer {
	var layers []middlewareLayer
	for _, router := range routers {
		router.mwMu.RLock()
		layers = append(layers, router.middlewares...)
		router.mwMu.RUnlock()
	}

	sort.Slice(layers, func(i, j int) bool {
		return layers[i].order < layers[j].order
	})

	return layers
}

// runChain executes middleware sequentially by wiring each layer's next()
// callback to the subsequent layer.
func runChain(chain []Middleware, owner *Sprout, w http.ResponseWriter, req *http.Request) {
	if len(chain) == 0 {
		return
	}

	var exec func(int, error)
	exec = func(idx int, err error) {
		if err != nil {
			if errors.Is(err, ErrNext) {
				err = nil
			}
			if err != nil {
				owner.handleChainError(w, req, err)
				return
			}
		}

		if idx >= len(chain) {
			return
		}
		chain[idx](w, req, func(nextErr error) {
			exec(idx+1, nextErr)
		})
	}

	exec(0, nil)
}

func (s *Sprout) handleChainError(w http.ResponseWriter, req *http.Request, err error) {
	if err == nil {
		return
	}

	if writeTypedErrorResponse(s, w, req, err, http.StatusInternalServerError) {
		return
	}

	handleError(s, w, req, err)
}

type contextKey string

const paramsContextKey contextKey = "sprout:params"

// withParams stores httprouter params on the request context so middleware and
// handlers can access them uniformly via Params().
func withParams(req *http.Request, ps httprouter.Params) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), paramsContextKey, ps))
}

// Params returns the route parameters for the current request.
func Params(r *http.Request) httprouter.Params {
	if value := r.Context().Value(paramsContextKey); value != nil {
		if params, ok := value.(httprouter.Params); ok {
			return params
		}
	}
	return nil
}

// ancestorChain returns routers from root → current so we can evaluate
// middleware inheritance in registration order.
func (s *Sprout) ancestorChain() []*Sprout {
	var chain []*Sprout
	for current := s; current != nil; current = current.parent {
		chain = append(chain, current)
	}

	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}

	return chain
}

// matchesPath reports whether the router's BasePath applies to the provided
// request path.
func (s *Sprout) matchesPath(path string) bool {
	base := s.config.BasePath
	if base == "" || base == "/" {
		return true
	}

	if !strings.HasPrefix(path, base) {
		return false
	}

	if len(path) == len(base) {
		return true
	}

	return path[len(base)] == '/'
}
