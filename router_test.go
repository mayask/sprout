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

// Test with path, query, and header parameters
type GetUserRequest struct {
	UserID    string `path:"id" validate:"required"`
	Page      int    `query:"page" validate:"omitempty,gte=1"`
	Limit     int    `query:"limit" validate:"omitempty,gte=1,lte=100"`
	AuthToken string `header:"Authorization" validate:"required"`
}

type GetUserResponse struct {
	UserID    string `json:"user_id" validate:"required"`
	Page      int    `json:"page" validate:"gte=0"`
	Limit     int    `json:"limit" validate:"gte=0"`
	AuthToken string `json:"auth_token" validate:"required"`
}

func TestSprouterWithPathQueryHeaders(t *testing.T) {
	router := New()
	GET(router, "/users/:id", func(ctx context.Context, req *GetUserRequest) (*GetUserResponse, error) {
		return &GetUserResponse{
			UserID:    req.UserID,
			Page:      req.Page,
			Limit:     req.Limit,
			AuthToken: req.AuthToken,
		}, nil
	})

	// Create request with path param, query params, and headers
	httpReq := httptest.NewRequest("GET", "/users/123?page=2&limit=50", nil)
	httpReq.Header.Set("Authorization", "Bearer token123")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httpReq)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status OK, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var resp GetUserResponse
	if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.UserID != "123" {
		t.Errorf("expected UserID '123', got '%s'", resp.UserID)
	}
	if resp.Page != 2 {
		t.Errorf("expected Page 2, got %d", resp.Page)
	}
	if resp.Limit != 50 {
		t.Errorf("expected Limit 50, got %d", resp.Limit)
	}
	if resp.AuthToken != "Bearer token123" {
		t.Errorf("expected AuthToken 'Bearer token123', got '%s'", resp.AuthToken)
	}
}

func TestSprouterMissingRequiredHeader(t *testing.T) {
	router := New()
	GET(router, "/users/:id", func(ctx context.Context, req *GetUserRequest) (*GetUserResponse, error) {
		return &GetUserResponse{
			UserID:    req.UserID,
			Page:      req.Page,
			Limit:     req.Limit,
			AuthToken: req.AuthToken,
		}, nil
	})

	// Create request without Authorization header
	httpReq := httptest.NewRequest("GET", "/users/123?page=2&limit=50", nil)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httpReq)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status BadRequest, got %d", recorder.Code)
	}
}

// Test combining body with path/query/headers
type UpdateUserRequest struct {
	UserID    string `path:"id" validate:"required"`
	AuthToken string `header:"Authorization" validate:"required"`
	Name      string `json:"name" validate:"required,min=3"`
	Email     string `json:"email" validate:"required,email"`
}

type UpdateUserResponse struct {
	UserID  string `json:"user_id" validate:"required"`
	Name    string `json:"name" validate:"required"`
	Email   string `json:"email" validate:"required"`
	Message string `json:"message" validate:"required"`
}

func TestSprouterWithBodyAndParams(t *testing.T) {
	router := New()
	PUT(router, "/users/:id", func(ctx context.Context, req *UpdateUserRequest) (*UpdateUserResponse, error) {
		return &UpdateUserResponse{
			UserID:  req.UserID,
			Name:    req.Name,
			Email:   req.Email,
			Message: "User updated",
		}, nil
	})

	// Create request with path param, header, and body
	reqBody := map[string]string{
		"name":  "Jane Doe",
		"email": "jane@example.com",
	}
	body, _ := json.Marshal(reqBody)

	httpReq := httptest.NewRequest("PUT", "/users/456", bytes.NewReader(body))
	httpReq.Header.Set("Authorization", "Bearer token456")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httpReq)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status OK, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var resp UpdateUserResponse
	if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.UserID != "456" {
		t.Errorf("expected UserID '456', got '%s'", resp.UserID)
	}
	if resp.Name != "Jane Doe" {
		t.Errorf("expected Name 'Jane Doe', got '%s'", resp.Name)
	}
	if resp.Email != "jane@example.com" {
		t.Errorf("expected Email 'jane@example.com', got '%s'", resp.Email)
	}
}
