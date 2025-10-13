package sprouter

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type EmptyRequest struct{}

type HelloResponse struct {
	Message string `json:"message" validate:"required"`
}

func TestSprouterBasic(t *testing.T) {
	router := New()
	GET(router, "/", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		return &HelloResponse{Message: "Hello, World!"}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status OK, got %d", recorder.Code)
	}

	var resp HelloResponse
	if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Message != "Hello, World!" {
		t.Errorf("expected message 'Hello, World!', got %s", resp.Message)
	}
}

type CreateUserRequest struct {
	Name  string `json:"name" validate:"required,min=3"`
	Email string `json:"email" validate:"required,email"`
}

type CreateUserResponse struct {
	ID    int    `json:"id" validate:"required,gt=0"`
	Name  string `json:"name" validate:"required"`
	Email string `json:"email" validate:"required,email"`
}

func TestSprouterWithValidation(t *testing.T) {
	router := New()
	POST(router, "/users", func(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
		return &CreateUserResponse{
			ID:    1,
			Name:  req.Name,
			Email: req.Email,
		}, nil
	})

	// Valid request
	reqBody := CreateUserRequest{
		Name:  "John Doe",
		Email: "john@example.com",
	}
	body, _ := json.Marshal(reqBody)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("POST", "/users", bytes.NewReader(body)))

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status OK, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var resp CreateUserResponse
	if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.ID != 1 || resp.Name != "John Doe" || resp.Email != "john@example.com" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestSprouterValidationFailure(t *testing.T) {
	router := New()
	POST(router, "/users", func(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
		return &CreateUserResponse{
			ID:    1,
			Name:  req.Name,
			Email: req.Email,
		}, nil
	})

	// Invalid request (name too short)
	reqBody := CreateUserRequest{
		Name:  "Jo",
		Email: "john@example.com",
	}
	body, _ := json.Marshal(reqBody)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("POST", "/users", bytes.NewReader(body)))

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status BadRequest, got %d", recorder.Code)
	}
}
