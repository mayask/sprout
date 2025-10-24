package sprout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiddlewareOrderBeforeRoute(t *testing.T) {
	router := New()
	var events []string

	router.Use(func(w http.ResponseWriter, r *http.Request, next Next) {
		events = append(events, "global-before")
		next(nil)
	})

	GET(router, "/hit", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		events = append(events, "route")
		return &HelloResponse{Message: "ok"}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/hit", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	if diff := cmpStringSlices(events, []string{"global-before", "route"}); diff != "" {
		t.Fatalf("unexpected event order: %s", diff)
	}
}

func TestMiddlewareAfterRouteWithoutNext(t *testing.T) {
	router := New()
	var events []string

	GET(router, "/hit", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		events = append(events, "route")
		return &HelloResponse{Message: "ok"}, nil
	})

	router.Use(func(w http.ResponseWriter, r *http.Request, next Next) {
		events = append(events, "global-after")
		next(nil)
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/hit", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	if diff := cmpStringSlices(events, []string{"route"}); diff != "" {
		t.Fatalf("unexpected event order: %s", diff)
	}
}

func TestMiddlewareAfterRouteWithNext(t *testing.T) {
	router := New()
	var events []string

	GET(router, "/hit", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		events = append(events, "route")
		return nil, ErrNext
	})

	router.Use(func(w http.ResponseWriter, r *http.Request, next Next) {
		events = append(events, "global-after")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("handled by middleware"))
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/hit", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	if diff := cmpStringSlices(events, []string{"route", "global-after"}); diff != "" {
		t.Fatalf("unexpected event order: %s", diff)
	}

	if recorder.Body.String() != "handled by middleware" {
		t.Fatalf("expected middleware body, got %q", recorder.Body.String())
	}
}

func TestGlobalFallbackMiddlewareRunsOnNotFound(t *testing.T) {
	router := New()
	var events []string

	GET(router, "/exact", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		events = append(events, "exact-route")
		return &HelloResponse{Message: "exact"}, nil
	})

	router.Use(func(w http.ResponseWriter, r *http.Request, next Next) {
		events = append(events, "global-fallback")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("fallback"))
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/other", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d: %s", recorder.Code, recorder.Body.String())
	}

	if recorder.Body.String() != "fallback" {
		t.Fatalf("expected fallback body, got %q", recorder.Body.String())
	}

	if diff := cmpStringSlices(events, []string{"global-fallback"}); diff != "" {
		t.Fatalf("unexpected event order: %s", diff)
	}
}

func TestChildMiddlewareIsolation(t *testing.T) {
	router := New()
	parent := router.Mount("/parent", nil)
	child := parent.Mount("/child", nil)
	sibling := parent.Mount("/sibling", nil)

	var events []string

	child.Use(func(w http.ResponseWriter, r *http.Request, next Next) {
		events = append(events, "child-middleware")
		next(nil)
	})

	GET(child, "/hit", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		events = append(events, "child-route")
		return &HelloResponse{Message: "child"}, nil
	})

	GET(sibling, "/hit", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		events = append(events, "sibling-route")
		return &HelloResponse{Message: "sibling"}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/parent/sibling/hit", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	if diff := cmpStringSlices(events, []string{"sibling-route"}); diff != "" {
		t.Fatalf("unexpected events: %s", diff)
	}
}

func TestChildMiddlewareOrder(t *testing.T) {
	router := New()
	parent := router.Mount("/parent", nil)
	child := parent.Mount("/child", nil)
	var events []string

	child.Use(func(w http.ResponseWriter, r *http.Request, next Next) {
		events = append(events, "child-middleware-before")
		next(nil)
	})

	GET(child, "/hit", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		events = append(events, "child-route")
		return &HelloResponse{Message: "child"}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/parent/child/hit", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	if diff := cmpStringSlices(events, []string{"child-middleware-before", "child-route"}); diff != "" {
		t.Fatalf("unexpected events: %s", diff)
	}
}

func TestChildMiddlewareAfterRouteWithNext(t *testing.T) {
	router := New()
	parent := router.Mount("/parent", nil)
	child := parent.Mount("/child", nil)
	var events []string

	GET(child, "/hit", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		events = append(events, "child-route")
		return nil, ErrNext
	})

	child.Use(func(w http.ResponseWriter, r *http.Request, next Next) {
		events = append(events, "child-middleware-after")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("child middleware"))
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/parent/child/hit", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	if diff := cmpStringSlices(events, []string{"child-route", "child-middleware-after"}); diff != "" {
		t.Fatalf("unexpected events: %s", diff)
	}
}

func TestChildMiddlewareNotTriggeredWhenPrefixMismatch(t *testing.T) {
	router := New()
	parent := router.Mount("/parent", nil)
	child := parent.Mount("/child", nil)
	var events []string

	child.Use(func(w http.ResponseWriter, r *http.Request, next Next) {
		events = append(events, "child-middleware")
		next(nil)
	})

	GET(child, "/hit", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		events = append(events, "child-route")
		return &HelloResponse{Message: "child"}, nil
	})

	GET(parent, "/other", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		events = append(events, "parent-route")
		return &HelloResponse{Message: "other"}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/parent/other", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	if diff := cmpStringSlices(events, []string{"parent-route"}); diff != "" {
		t.Fatalf("unexpected events: %s", diff)
	}
}

func TestMiddlewareNextWithTypedError(t *testing.T) {
	router := New()
	var routeCalled bool

	router.Use(func(w http.ResponseWriter, r *http.Request, next Next) {
		next(&TeapotError{Msg: "brew fail"})
	})

	GET(router, "/tea", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		routeCalled = true
		return &HelloResponse{Message: "tea"}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/tea", nil))

	if routeCalled {
		t.Fatalf("route should not run when middleware raises an error")
	}

	if recorder.Code != http.StatusTeapot {
		t.Fatalf("expected status 418, got %d", recorder.Code)
	}

	var payload map[string]string
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if payload["message"] != "brew fail" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
}

func TestMiddlewareNextWithGenericErrorUsesErrorHandler(t *testing.T) {
	handled := false
	router := NewWithConfig(&Config{
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			handled = true
			http.Error(w, "custom: "+err.Error(), http.StatusBadRequest)
		},
	})

	router.Use(func(w http.ResponseWriter, r *http.Request, next Next) {
		next(errors.New("boom"))
	})

	GET(router, "/hit", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		t.Fatalf("handler should not run after middleware error")
		return nil, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/hit", nil))

	if !handled {
		t.Fatalf("expected custom error handler to run")
	}

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}

	if body := strings.TrimSpace(recorder.Body.String()); !strings.Contains(body, "custom: boom") {
		t.Fatalf("unexpected body: %q", body)
	}
}

func cmpStringSlices(actual, expected []string) string {
	if len(actual) != len(expected) {
		return fmt.Sprintf("length mismatch: actual=%v expected=%v", actual, expected)
	}
	for i := range actual {
		if actual[i] != expected[i] {
			return fmt.Sprintf("index %d: actual=%q expected=%q (full actual=%v expected=%v)", i, actual[i], expected[i], actual, expected)
		}
	}
	return ""
}
