package sprout

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type customDecodedBody struct {
	Value string `json:"value" validate:"required"`
}

type customDecodedRequest struct {
	Body *customDecodedBody `body:"" contentType:"application/x-sprout-test" validate:"required"`
}

func TestRegisterRequestBodyDecoderReceivesReaderParamsAndCleansUp(t *testing.T) {
	router := New()
	cleanupCalled := false
	paramsSeen := false

	router.RegisterRequestBodyDecoder(
		"application/x-sprout-test",
		func(_ context.Context, body io.Reader, params map[string]string, target any) (func() error, *Error) {
			paramsSeen = params["version"] == "1"
			payload, err := io.ReadAll(body)
			if err != nil {
				return nil, &Error{Kind: ErrorKindParse, Message: "read failed", Err: err}
			}
			target.(*customDecodedBody).Value = string(payload)
			return func() error {
				cleanupCalled = true
				return nil
			}, nil
		},
	)

	child := router.Mount("/api", nil)
	POST(child, "/decode", func(_ context.Context, req *customDecodedRequest) (*streamBodyResponse, error) {
		return &streamBodyResponse{Content: req.Body.Value}, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/api/decode", strings.NewReader("decoded"))
	req.Header.Set("Content-Type", "application/x-sprout-test; version=1")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status OK, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if !paramsSeen {
		t.Fatal("expected parsed content-type parameters")
	}
	if !cleanupCalled {
		t.Fatal("expected decoder cleanup after handler")
	}
}

func TestRegisterRequestBodyDecoderCleanupRunsOnDecodeError(t *testing.T) {
	router := New()
	cleanupCalled := false

	router.RegisterRequestBodyDecoder(
		"application/x-sprout-test",
		func(_ context.Context, _ io.Reader, _ map[string]string, _ any) (func() error, *Error) {
			return func() error {
				cleanupCalled = true
				return nil
			}, &Error{Kind: ErrorKindParse, Message: "decode failed"}
		},
	)

	POST(router, "/decode", func(_ context.Context, _ *customDecodedRequest) (*streamBodyResponse, error) {
		t.Fatal("handler must not run after decode error")
		return nil, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/decode", strings.NewReader("bad"))
	req.Header.Set("Content-Type", "application/x-sprout-test")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status BadRequest, got %d", recorder.Code)
	}
	if !cleanupCalled {
		t.Fatal("expected decoder cleanup after decode error")
	}
}
