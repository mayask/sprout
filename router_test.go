package sprouter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSprouter(t *testing.T) {
	router := New()
	router.GET("/", func(ctx context.Context, req any) (any, error) {
		return "Hello, World!", nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status OK, got %d", recorder.Code)
	}
	if recorder.Body.String() != "Hello, World!" {
		t.Errorf("expected body 'Hello, World!', got %s", recorder.Body.String())
	}
}
