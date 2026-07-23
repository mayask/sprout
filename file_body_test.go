package sprout

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
)

type trackingReadSeekCloser struct {
	*bytes.Reader
	closeCount int
}

func (r *trackingReadSeekCloser) Close() error {
	r.closeCount++
	return nil
}

type fileBodyRequest struct{}

type fileBodyResponse struct {
	ContentDisposition string    `header:"Content-Disposition" validate:"required"`
	Body               *FileBody `body:"" contentType:"application/octet-stream" validate:"required"`
}

type fileBodyValueResponse struct {
	Body FileBody `body:"" contentType:"application/octet-stream" validate:"required"`
}

type invalidFileBodyResponse struct {
	Missing string    `json:"missing" validate:"required"`
	Body    *FileBody `body:"" contentType:"application/octet-stream" validate:"required"`
}

func newFileBodyResponse(content *trackingReadSeekCloser) *fileBodyResponse {
	return &fileBodyResponse{
		ContentDisposition: `attachment; filename="report.csv"`,
		Body: NewFileBody(
			content,
			"report.csv",
			time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC),
		),
	}
}

func TestFileBodyStreamsContentHeadersAndCloses(t *testing.T) {
	router := New()
	content := &trackingReadSeekCloser{Reader: bytes.NewReader([]byte("id,amount\n1,100\n"))}

	GET(router, "/download", func(_ context.Context, _ *fileBodyRequest) (*fileBodyResponse, error) {
		return newFileBodyResponse(content), nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/download", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status OK, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.String() != "id,amount\n1,100\n" {
		t.Fatalf("unexpected file body %q", recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("expected application/octet-stream, got %q", got)
	}
	if got := recorder.Header().Get("Content-Disposition"); got != `attachment; filename="report.csv"` {
		t.Fatalf("unexpected content disposition %q", got)
	}
	if content.closeCount != 1 {
		t.Fatalf("expected file content to close exactly once, got %d", content.closeCount)
	}
}

func TestFileBodySupportsRangeRequests(t *testing.T) {
	router := New()
	content := &trackingReadSeekCloser{Reader: bytes.NewReader([]byte("0123456789"))}

	GET(router, "/download", func(_ context.Context, _ *fileBodyRequest) (*fileBodyResponse, error) {
		return newFileBodyResponse(content), nil
	})

	req := httptest.NewRequest(http.MethodGet, "/download", nil)
	req.Header.Set("Range", "bytes=2-4")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusPartialContent {
		t.Fatalf("expected status PartialContent, got %d", recorder.Code)
	}
	if recorder.Body.String() != "234" {
		t.Fatalf("expected ranged body 234, got %q", recorder.Body.String())
	}
	if content.closeCount != 1 {
		t.Fatalf("expected ranged content to close exactly once, got %d", content.closeCount)
	}
}

func TestFileBodyClosesOnceOnResponseValidationFailure(t *testing.T) {
	router := New()
	content := &trackingReadSeekCloser{Reader: bytes.NewReader([]byte("content"))}

	GET(router, "/download", func(_ context.Context, _ *fileBodyRequest) (*invalidFileBodyResponse, error) {
		return &invalidFileBodyResponse{
			Body: NewFileBody(content, "report.csv", time.Time{}),
		}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/download", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status InternalServerError, got %d", recorder.Code)
	}
	if content.closeCount != 1 {
		t.Fatalf("expected validation-failed content to close exactly once, got %d", content.closeCount)
	}
}

func TestFileBodyClosesOnceWhenHandlerReturnsResponseAndError(t *testing.T) {
	strict := false
	router := NewWithConfig(&Config{StrictErrorTypes: &strict})
	content := &trackingReadSeekCloser{Reader: bytes.NewReader([]byte("content"))}

	GET(router, "/download", func(_ context.Context, _ *fileBodyRequest) (*fileBodyResponse, error) {
		return newFileBodyResponse(content), errors.New("handler failed")
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/download", nil))

	if content.closeCount != 1 {
		t.Fatalf("expected errored response content to close exactly once, got %d", content.closeCount)
	}
}

func TestFileBodyValueFieldRejectedAtRegistration(t *testing.T) {
	router := New()
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("expected value-form FileBody route registration to panic")
		}
	}()

	GET(router, "/download", func(_ context.Context, _ *fileBodyRequest) (*fileBodyValueResponse, error) {
		return &fileBodyValueResponse{}, nil
	})
}

func TestFileBodyOpenAPIResponse(t *testing.T) {
	router := New()
	GET(router, "/download", func(_ context.Context, _ *fileBodyRequest) (*fileBodyResponse, error) {
		content := &trackingReadSeekCloser{Reader: bytes.NewReader([]byte("content"))}
		return newFileBodyResponse(content), nil
	})

	specJSON, err := router.OpenAPIJSON()
	if err != nil {
		t.Fatalf("failed to generate OpenAPI: %v", err)
	}
	var doc openapi3.T
	if err := json.Unmarshal(specJSON, &doc); err != nil {
		t.Fatalf("failed to parse OpenAPI: %v", err)
	}

	response := doc.Paths.Value("/download").Get.Responses.Value("200")
	if response == nil || response.Value == nil {
		t.Fatal("expected 200 response")
	}
	media := response.Value.Content["application/octet-stream"]
	if media == nil || media.Schema == nil || media.Schema.Value == nil {
		t.Fatal("expected binary response schema")
	}
	if media.Schema.Value.Type == nil || !media.Schema.Value.Type.Is("string") {
		t.Fatalf("expected string schema, got %#v", media.Schema.Value.Type)
	}
	if media.Schema.Value.Format != "binary" {
		t.Fatalf("expected binary format, got %q", media.Schema.Value.Format)
	}
	if response.Value.Content["application/json"] != nil {
		t.Fatal("did not expect application/json response content")
	}
}
