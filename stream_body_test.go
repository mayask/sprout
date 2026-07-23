package sprout

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

type streamBodyRequest struct {
	Column *int        `query:"column" validate:"required,gte=0"`
	Body   *StreamBody `body:"" contentType:"text/csv" validate:"required"`
}

type streamBodyValueRequest struct {
	Body StreamBody `body:"" contentType:"text/csv" validate:"required"`
}

type streamBodyResponse struct {
	Content string `json:"content" validate:"required"`
	Column  int    `json:"column"`
}

type trackingReadCloser struct {
	io.Reader
	closed bool
}

func (r *trackingReadCloser) Close() error {
	r.closed = true
	return nil
}

func TestStreamBodyValueFieldRejectedAtRegistration(t *testing.T) {
	router := New()

	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("expected value-form StreamBody route registration to panic")
		}
	}()

	POST(router, "/upload", func(_ context.Context, _ *streamBodyValueRequest) (*streamBodyResponse, error) {
		return &streamBodyResponse{Content: "unexpected"}, nil
	})
}

func TestStreamBodyBindsQueryAndRemainsLiveForHandler(t *testing.T) {
	router := New()
	var readsBeforeHandler int

	POST(router, "/upload", func(_ context.Context, req *streamBodyRequest) (*streamBodyResponse, error) {
		if req.Body.ContentType() != "text/csv" {
			t.Fatalf("expected normalized text/csv content type, got %q", req.Body.ContentType())
		}
		content, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("failed to read stream body: %v", err)
		}
		readsBeforeHandler = len(content)
		return &streamBodyResponse{Content: string(content), Column: *req.Column}, nil
	})

	body := &trackingReadCloser{Reader: strings.NewReader("a,b\n1,2\n")}
	req := httptest.NewRequest(http.MethodPost, "/upload?column=0", body)
	req.Header.Set("Content-Type", "text/csv; charset=utf-8")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status OK, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if readsBeforeHandler == 0 {
		t.Fatal("expected handler to consume the live body")
	}
	if !body.closed {
		t.Fatal("expected Sprout to close the request body after the handler")
	}

	var response streamBodyResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response.Content != "a,b\n1,2\n" || response.Column != 0 {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestStreamBodyRejectsMissingQueryBeforeHandler(t *testing.T) {
	router := New()
	handlerCalled := false

	POST(router, "/upload", func(_ context.Context, _ *streamBodyRequest) (*streamBodyResponse, error) {
		handlerCalled = true
		return &streamBodyResponse{}, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader("a,b\n"))
	req.Header.Set("Content-Type", "text/csv")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status BadRequest, got %d", recorder.Code)
	}
	if handlerCalled {
		t.Fatal("expected handler not to be called")
	}
}

func TestStreamBodyRejectsEmptyChunkedBody(t *testing.T) {
	router := New()
	handlerCalled := false
	body := &trackingReadCloser{Reader: strings.NewReader("")}

	POST(router, "/upload", func(_ context.Context, _ *streamBodyRequest) (*streamBodyResponse, error) {
		handlerCalled = true
		return &streamBodyResponse{}, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/upload?column=1", body)
	req.ContentLength = -1
	req.Header.Set("Content-Type", "text/csv")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status BadRequest, got %d", recorder.Code)
	}
	if handlerCalled {
		t.Fatal("expected handler not to be called for empty required body")
	}
	if !body.closed {
		t.Fatal("expected empty body to be closed")
	}
}

func TestStreamBodyRejectsWrongContentType(t *testing.T) {
	router := New()
	handlerCalled := false

	POST(router, "/upload", func(_ context.Context, _ *streamBodyRequest) (*streamBodyResponse, error) {
		handlerCalled = true
		return &streamBodyResponse{}, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/upload?column=1", strings.NewReader(`{"a":1}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status BadRequest, got %d", recorder.Code)
	}
	if handlerCalled {
		t.Fatal("expected handler not to be called")
	}
}

func TestStreamBodyHonorsRouteBodyLimit(t *testing.T) {
	router := New()
	var limitErr error

	POST(router, "/upload", func(_ context.Context, req *streamBodyRequest) (*streamBodyResponse, error) {
		_, limitErr = io.ReadAll(req.Body)
		return &streamBodyResponse{Content: "checked", Column: *req.Column}, nil
	}, WithRequestBodyLimit(4))

	req := httptest.NewRequest(http.MethodPost, "/upload?column=1", strings.NewReader("12345"))
	req.Header.Set("Content-Type", "text/csv")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	var maxErr *http.MaxBytesError
	if !errors.As(limitErr, &maxErr) {
		t.Fatalf("expected MaxBytesError, got %v", limitErr)
	}
}

func TestStreamBodyClosesRejectedContentType(t *testing.T) {
	router := New()
	body := &trackingReadCloser{Reader: strings.NewReader(`{"a":1}`)}

	POST(router, "/upload", func(_ context.Context, _ *streamBodyRequest) (*streamBodyResponse, error) {
		return &streamBodyResponse{}, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/upload?column=1", body)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if !body.closed {
		t.Fatal("expected rejected body to be closed")
	}
}

func TestStreamBodyClosesAfterHandlerError(t *testing.T) {
	router := New()
	body := &trackingReadCloser{Reader: strings.NewReader("a,b\n")}

	POST(router, "/upload", func(_ context.Context, _ *streamBodyRequest) (*streamBodyResponse, error) {
		return nil, errors.New("handler failed")
	})

	req := httptest.NewRequest(http.MethodPost, "/upload?column=1", body)
	req.Header.Set("Content-Type", "text/csv")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if !body.closed {
		t.Fatal("expected body to be closed after handler error")
	}
}

func TestStreamBodyOpenAPIRequestBody(t *testing.T) {
	router := New()
	POST(router, "/upload", func(_ context.Context, _ *streamBodyRequest) (*streamBodyResponse, error) {
		return &streamBodyResponse{Content: "ok"}, nil
	})

	specJSON, err := router.OpenAPIJSON()
	if err != nil {
		t.Fatalf("failed to generate OpenAPI: %v", err)
	}
	var doc openapi3.T
	if err := json.Unmarshal(specJSON, &doc); err != nil {
		t.Fatalf("failed to parse OpenAPI: %v", err)
	}

	op := doc.Paths.Value("/upload").Post
	if op == nil || op.RequestBody == nil || op.RequestBody.Value == nil {
		t.Fatal("expected request body")
	}
	if !op.RequestBody.Value.Required {
		t.Fatal("expected required request body")
	}
	media := op.RequestBody.Value.Content["text/csv"]
	if media == nil || media.Schema == nil || media.Schema.Value == nil {
		t.Fatal("expected text/csv request body schema")
	}
	if media.Schema.Value.Type == nil || !media.Schema.Value.Type.Is("string") {
		t.Fatalf("expected string schema, got %#v", media.Schema.Value.Type)
	}
	if media.Schema.Value.Format != "binary" {
		t.Fatalf("expected binary format, got %q", media.Schema.Value.Format)
	}

	if len(op.Parameters) != 1 || op.Parameters[0].Value.Name != "column" || !op.Parameters[0].Value.Required {
		t.Fatalf("expected required column query parameter, got %#v", op.Parameters)
	}
}
