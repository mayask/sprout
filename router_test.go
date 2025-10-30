package sprout

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type EmptyRequest struct{}

type bodyTrackingRecorder struct {
	*httptest.ResponseRecorder
	wroteBody bool
}

func newBodyTrackingRecorder() *bodyTrackingRecorder {
	return &bodyTrackingRecorder{ResponseRecorder: httptest.NewRecorder()}
}

func (r *bodyTrackingRecorder) Write(b []byte) (int, error) {
	r.wroteBody = true
	return r.ResponseRecorder.Write(b)
}

type HelloResponse struct {
	Message string `json:"message" validate:"required"`
}

type TeapotError struct {
	_   struct{} `http:"status=418"`
	Msg string   `json:"message" validate:"required"`
}

func (e *TeapotError) Error() string {
	return e.Msg
}

func TestSproutBasic(t *testing.T) {
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

func TestSproutWithValidation(t *testing.T) {
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

type ListUsersResponse struct {
	ID    int    `json:"id" validate:"required"`
	Email string `json:"email" validate:"required,email"`
}

type ListUsersEnvelope struct {
	Users []ListUsersResponse `json:"users" sprout:"unwrap" validate:"required,dive"`
}

func TestSproutSliceResponse(t *testing.T) {
	router := New()
	GET(router, "/users", func(ctx context.Context, req *EmptyRequest) (*ListUsersEnvelope, error) {
		return &ListUsersEnvelope{
			Users: []ListUsersResponse{
				{ID: 1, Email: "alice@example.com"},
				{ID: 2, Email: "bob@example.com"},
			},
		}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/users", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status OK, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var resp []ListUsersResponse
	if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp) != 2 {
		t.Fatalf("expected two users, got %d", len(resp))
	}
	if resp[0].ID != 1 || resp[0].Email != "alice@example.com" {
		t.Errorf("unexpected first user: %+v", resp[0])
	}
	if resp[1].ID != 2 || resp[1].Email != "bob@example.com" {
		t.Errorf("unexpected second user: %+v", resp[1])
	}
}

func TestSproutSliceResponseValidationFailure(t *testing.T) {
	router := New()
	GET(router, "/users", func(ctx context.Context, req *EmptyRequest) (*ListUsersEnvelope, error) {
		envelope := &ListUsersEnvelope{
			Users: []ListUsersResponse{
				{ID: 1, Email: "invalid-email"},
				{ID: 2, Email: "bob@example.com"},
			},
		}
		return envelope, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/users", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status InternalServerError, got %d", recorder.Code)
	}

	if !strings.Contains(recorder.Body.String(), "response validation failed") {
		t.Fatalf("expected response validation error message, got %q", recorder.Body.String())
	}
}

func TestSproutValidationFailure(t *testing.T) {
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

func TestSproutWithPathQueryHeaders(t *testing.T) {
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

func TestSproutMissingRequiredHeader(t *testing.T) {
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

func TestSproutWithBodyAndParams(t *testing.T) {
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

// Test error handling with typed errors

type NotFoundError struct {
	_        struct{} `http:"status=404"`
	Resource string   `json:"resource" validate:"required"`
	Message  string   `json:"message" validate:"required"`
}

func (e NotFoundError) Error() string {
	return e.Message
}

type ConflictError struct {
	_       struct{} `http:"status=409"`
	Field   string   `json:"field" validate:"required"`
	Message string   `json:"message" validate:"required"`
}

func (e ConflictError) Error() string {
	return e.Message
}

type ValidationError struct {
	_       struct{} `http:"status=400"`
	Fields  []string `json:"fields" validate:"required,min=1"`
	Message string   `json:"message" validate:"required"`
}

func (e ValidationError) Error() string {
	return e.Message
}

func TestSproutHTTPError(t *testing.T) {
	router := New()

	// Register handler with expected error types
	POST(router, "/items", func(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
		// Simulate not found error
		if req.Name == "notfound" {
			return nil, NotFoundError{
				Resource: "user",
				Message:  "user not found",
			}
		}

		// Simulate conflict error
		if req.Name == "conflict" {
			return nil, ConflictError{
				Field:   "email",
				Message: "email already exists",
			}
		}

		return &CreateUserResponse{
			ID:    1,
			Name:  req.Name,
			Email: req.Email,
		}, nil
	}, WithErrors(NotFoundError{}, ConflictError{}, ValidationError{}))

	// Test NotFoundError
	t.Run("NotFoundError", func(t *testing.T) {
		reqBody := CreateUserRequest{
			Name:  "notfound",
			Email: "test@example.com",
		}
		body, _ := json.Marshal(reqBody)

		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("POST", "/items", bytes.NewReader(body)))

		if recorder.Code != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", recorder.Code)
		}

		var errResp NotFoundError
		if err := json.NewDecoder(recorder.Body).Decode(&errResp); err != nil {
			t.Fatalf("failed to decode error response: %v", err)
		}

		if errResp.Resource != "user" {
			t.Errorf("expected resource 'user', got '%s'", errResp.Resource)
		}
	})

	// Test ConflictError
	t.Run("ConflictError", func(t *testing.T) {
		reqBody := CreateUserRequest{
			Name:  "conflict",
			Email: "test@example.com",
		}
		body, _ := json.Marshal(reqBody)

		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("POST", "/items", bytes.NewReader(body)))

		if recorder.Code != http.StatusConflict {
			t.Errorf("expected status 409, got %d", recorder.Code)
		}

		var errResp ConflictError
		if err := json.NewDecoder(recorder.Body).Decode(&errResp); err != nil {
			t.Fatalf("failed to decode error response: %v", err)
		}

		if errResp.Field != "email" {
			t.Errorf("expected field 'email', got '%s'", errResp.Field)
		}
	})

	// Test success case
	t.Run("Success", func(t *testing.T) {
		reqBody := CreateUserRequest{
			Name:  "John Doe",
			Email: "john@example.com",
		}
		body, _ := json.Marshal(reqBody)

		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("POST", "/items", bytes.NewReader(body)))

		if recorder.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d: %s", recorder.Code, recorder.Body.String())
		}
	})
}

func TestGlobalErrorHandlerReceivesUndeclaredError(t *testing.T) {
	var called bool

	router := NewWithConfig(&Config{
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			t.Helper()
			var sproutErr *Error
			if !errors.As(err, &sproutErr) {
				t.Fatalf("expected error to be *sprout.Error, got %T", err)
			}
			if sproutErr.Kind != ErrorKindUndeclaredError {
				t.Fatalf("expected ErrorKindUndeclaredError, got %s", sproutErr.Kind)
			}
			called = true
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("handled"))
		},
	})

	GET(router, "/boom", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		return nil, &TeapotError{Msg: "boom"}
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/boom", nil))

	if !called {
		t.Fatalf("expected global error handler to be called for undeclared error but it was not")
	}

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500 from custom handler, got %d", recorder.Code)
	}

	if body := recorder.Body.String(); body != "handled" {
		t.Fatalf("expected body 'handled', got %q", body)
	}
}

func TestGlobalErrorHandlerOverridesResponse(t *testing.T) {
	router := NewWithConfig(&Config{
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			t.Helper()
			var sproutErr *Error
			if !errors.As(err, &sproutErr) {
				t.Fatalf("expected error to be *sprout.Error, got %T", err)
			}
			if sproutErr.Kind != ErrorKindUndeclaredError {
				t.Fatalf("expected ErrorKindUndeclaredError, got %s", sproutErr.Kind)
			}
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("custom override"))
		},
	})

	GET(router, "/override", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		return nil, &TeapotError{Msg: "boom"}
	}, WithErrors(NotFoundError{}))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/override", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected overridden status 500, got %d", recorder.Code)
	}

	if body := recorder.Body.String(); body != "custom override" {
		t.Fatalf("expected overridden body 'custom override', got %q", body)
	}

	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/plain" {
		t.Fatalf("expected overridden Content-Type 'text/plain', got %q", contentType)
	}
}

func TestGlobalErrorHandlerNonStrictReceivesOriginalError(t *testing.T) {
	strict := false
	var received error

	router := NewWithConfig(&Config{
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			received = err
			w.WriteHeader(http.StatusInternalServerError)
		},
		StrictErrorTypes: &strict,
	})

	GET(router, "/boom", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		return nil, &TeapotError{Msg: "boom"}
	}, WithErrors(NotFoundError{}))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/boom", nil))

	if received == nil {
		t.Fatalf("expected global error handler to receive error")
	}

	var teapot *TeapotError
	if !errors.As(received, &teapot) {
		t.Fatalf("expected original TeapotError in non-strict mode, got %T", received)
	}

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500 from custom handler, got %d", recorder.Code)
	}
}

func TestDeclaredErrorSkipsErrorHandler(t *testing.T) {
	var called bool
	router := NewWithConfig(&Config{
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			called = true
		},
	})

	GET(router, "/declared", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		return nil, NotFoundError{
			Resource: "user",
			Message:  "user not found",
		}
	}, WithErrors(NotFoundError{}))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/declared", nil))

	if called {
		t.Fatalf("expected declared typed error to skip error handler")
	}

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", recorder.Code)
	}
}

func TestDeclaredInvalidErrorNonStrictSkipsErrorHandler(t *testing.T) {
	strict := false
	var called bool

	router := NewWithConfig(&Config{
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			called = true
		},
		StrictErrorTypes: &strict,
	})

	GET(router, "/invalid-declared", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		return nil, NotFoundError{
			Resource: "user",
			Message:  "", // invalid per validation rules
		}
	}, WithErrors(NotFoundError{}))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/invalid-declared", nil))

	if called {
		t.Fatalf("expected non-strict declared error to skip error handler despite validation failure")
	}

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", recorder.Code)
	}
}

func TestUndeclaredInvalidErrorNonStrictHitsHandlerWithOriginalError(t *testing.T) {
	strict := false
	var captured error

	router := NewWithConfig(&Config{
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			captured = err
			w.WriteHeader(http.StatusInternalServerError)
		},
		StrictErrorTypes: &strict,
	})

	GET(router, "/undeclared-invalid", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		return nil, NotFoundError{
			Resource: "user",
			Message:  "", // invalid
		}
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/undeclared-invalid", nil))

	if captured == nil {
		t.Fatalf("expected error handler to capture original error")
	}

	var notFound NotFoundError
	if !errors.As(captured, &notFound) {
		t.Fatalf("expected error handler to receive NotFoundError, got %T", captured)
	}

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500 from handler, got %d", recorder.Code)
	}
}

func TestSproutWithoutErrorHints(t *testing.T) {
	router := New()

	// Register handler without error hints (still works)
	GET(router, "/legacy", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		return &HelloResponse{Message: "Legacy endpoint"}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/legacy", nil))

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status OK, got %d", recorder.Code)
	}
}

func TestErrorResponseValidation(t *testing.T) {
	router := New()

	// Handler that returns invalid error (missing required fields)
	POST(router, "/invalid-error", func(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
		// Return error with missing required field (Message is empty)
		return nil, NotFoundError{
			Resource: "user",
			Message:  "", // Invalid! Message is required
		}
	}, WithErrors(NotFoundError{}))

	reqBody := CreateUserRequest{
		Name:  "Test User",
		Email: "test@example.com",
	}
	body, _ := json.Marshal(reqBody)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("POST", "/invalid-error", bytes.NewReader(body)))

	// Should return 500 because error validation failed
	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500 (validation failed), got %d", recorder.Code)
	}

	if !bytes.Contains(recorder.Body.Bytes(), []byte("error response validation failed")) {
		t.Errorf("expected validation error message, got: %s", recorder.Body.String())
	}
}

func TestValidErrorResponseValidation(t *testing.T) {
	router := New()

	// Handler that returns valid error (all required fields present)
	POST(router, "/valid-error", func(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
		return nil, NotFoundError{
			Resource: "user",
			Message:  "user not found",
		}
	}, WithErrors(NotFoundError{}))

	reqBody := CreateUserRequest{
		Name:  "Test User",
		Email: "test@example.com",
	}
	body, _ := json.Marshal(reqBody)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("POST", "/valid-error", bytes.NewReader(body)))

	// Should return 404 because error is valid
	if recorder.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var errResp NotFoundError
	if err := json.NewDecoder(recorder.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	if errResp.Resource != "user" || errResp.Message != "user not found" {
		t.Errorf("unexpected error response: %+v", errResp)
	}
}

func TestHandle(t *testing.T) {
	router := New()

	// Use handle directly for custom HTTP method
	handle(router, "CUSTOM", "/custom", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		return &HelloResponse{Message: "Custom method works!"}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("CUSTOM", "/custom", nil))

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status OK, got %d", recorder.Code)
	}

	var resp HelloResponse
	if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Message != "Custom method works!" {
		t.Errorf("expected message 'Custom method works!', got %s", resp.Message)
	}
}

// Test custom success status codes
type CreatedResponse struct {
	_       struct{} `http:"status=201"`
	ID      int      `json:"id" validate:"required,gt=0"`
	Message string   `json:"message" validate:"required"`
}

type AcceptedResponse struct {
	_       struct{} `http:"status=202"`
	JobID   string   `json:"job_id" validate:"required"`
	Message string   `json:"message" validate:"required"`
}

func TestCustomSuccessStatusCodes(t *testing.T) {
	router := New()

	// Test 201 Created
	POST(router, "/items", func(ctx context.Context, req *EmptyRequest) (*CreatedResponse, error) {
		return &CreatedResponse{
			ID:      42,
			Message: "Item created",
		}, nil
	})

	// Test 202 Accepted
	POST(router, "/jobs", func(ctx context.Context, req *EmptyRequest) (*AcceptedResponse, error) {
		return &AcceptedResponse{
			JobID:   "job-123",
			Message: "Job accepted for processing",
		}, nil
	})

	// Test 201 Created
	t.Run("Created201", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("POST", "/items", nil))

		if recorder.Code != http.StatusCreated {
			t.Errorf("expected status 201 Created, got %d", recorder.Code)
		}

		var resp CreatedResponse
		if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.ID != 42 {
			t.Errorf("expected ID 42, got %d", resp.ID)
		}
		if resp.Message != "Item created" {
			t.Errorf("expected message 'Item created', got '%s'", resp.Message)
		}
	})

	// Test 202 Accepted
	t.Run("Accepted202", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("POST", "/jobs", nil))

		if recorder.Code != http.StatusAccepted {
			t.Errorf("expected status 202 Accepted, got %d", recorder.Code)
		}

		var resp AcceptedResponse
		if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.JobID != "job-123" {
			t.Errorf("expected JobID 'job-123', got '%s'", resp.JobID)
		}
		if resp.Message != "Job accepted for processing" {
			t.Errorf("expected message 'Job accepted for processing', got '%s'", resp.Message)
		}
	})
}

// Test custom headers
type HeaderResponse struct {
	_            struct{} `http:"status=200"`
	CustomHeader string   `header:"X-Custom-Header"`
	ApiVersion   string   `header:"X-Api-Version"`
	Message      string   `json:"message" validate:"required"`
}

type HeaderErrorResponse struct {
	_         struct{} `http:"status=400"`
	ErrorCode string   `header:"X-Error-Code"`
	RequestID string   `header:"X-Request-Id"`
	Message   string   `json:"message" validate:"required"`
}

func (e HeaderErrorResponse) Error() string {
	return e.Message
}

func TestCustomHeaders(t *testing.T) {
	router := New()

	// Test custom headers on success response
	GET(router, "/with-headers", func(ctx context.Context, req *EmptyRequest) (*HeaderResponse, error) {
		return &HeaderResponse{
			CustomHeader: "CustomValue",
			ApiVersion:   "v1",
			Message:      "Success with custom headers",
		}, nil
	})

	// Test custom headers on error response
	GET(router, "/with-error-headers", func(ctx context.Context, req *EmptyRequest) (*HeaderResponse, error) {
		return nil, HeaderErrorResponse{
			ErrorCode: "INVALID_INPUT",
			RequestID: "req-123",
			Message:   "Error with custom headers",
		}
	}, WithErrors(HeaderErrorResponse{}))

	// Test success response headers
	t.Run("SuccessResponseHeaders", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", "/with-headers", nil))

		if recorder.Code != http.StatusOK {
			t.Errorf("expected status 200 OK, got %d", recorder.Code)
		}

		// Check custom headers
		if header := recorder.Header().Get("X-Custom-Header"); header != "CustomValue" {
			t.Errorf("expected X-Custom-Header 'CustomValue', got '%s'", header)
		}
		if header := recorder.Header().Get("X-Api-Version"); header != "v1" {
			t.Errorf("expected X-Api-Version 'v1', got '%s'", header)
		}

		var resp HeaderResponse
		if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.Message != "Success with custom headers" {
			t.Errorf("expected message 'Success with custom headers', got '%s'", resp.Message)
		}
	})

	// Test error response headers
	t.Run("ErrorResponseHeaders", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", "/with-error-headers", nil))

		if recorder.Code != http.StatusBadRequest {
			t.Errorf("expected status 400 Bad Request, got %d", recorder.Code)
		}

		// Check custom headers
		if header := recorder.Header().Get("X-Error-Code"); header != "INVALID_INPUT" {
			t.Errorf("expected X-Error-Code 'INVALID_INPUT', got '%s'", header)
		}
		if header := recorder.Header().Get("X-Request-Id"); header != "req-123" {
			t.Errorf("expected X-Request-Id 'req-123', got '%s'", header)
		}

		var errResp HeaderErrorResponse
		if err := json.NewDecoder(recorder.Body).Decode(&errResp); err != nil {
			t.Fatalf("failed to decode error response: %v", err)
		}

		if errResp.Message != "Error with custom headers" {
			t.Errorf("expected message 'Error with custom headers', got '%s'", errResp.Message)
		}
	})
}

// Test custom Content-Type header
type CustomContentTypeResponse struct {
	_           struct{} `http:"status=200"`
	ContentType string   `header:"Content-Type"`
	Message     string   `json:"message" validate:"required"`
}

type CustomContentTypeError struct {
	_           struct{} `http:"status=400"`
	ContentType string   `header:"Content-Type"`
	Message     string   `json:"message" validate:"required"`
}

func (e CustomContentTypeError) Error() string {
	return e.Message
}

func TestCustomContentType(t *testing.T) {
	router := New()

	// Test default Content-Type (application/json)
	GET(router, "/default-content-type", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		return &HelloResponse{Message: "Default content type"}, nil
	})

	// Test custom Content-Type on success response
	GET(router, "/custom-content-type", func(ctx context.Context, req *EmptyRequest) (*CustomContentTypeResponse, error) {
		return &CustomContentTypeResponse{
			ContentType: "application/vnd.api+json",
			Message:     "Custom content type",
		}, nil
	})

	// Test custom Content-Type on error response
	GET(router, "/custom-error-content-type", func(ctx context.Context, req *EmptyRequest) (*CustomContentTypeResponse, error) {
		return nil, CustomContentTypeError{
			ContentType: "application/problem+json",
			Message:     "Custom error content type",
		}
	}, WithErrors(CustomContentTypeError{}))

	// Test default Content-Type
	t.Run("DefaultContentType", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", "/default-content-type", nil))

		if recorder.Code != http.StatusOK {
			t.Errorf("expected status 200 OK, got %d", recorder.Code)
		}

		// Should have default Content-Type
		if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
			t.Errorf("expected Content-Type 'application/json', got '%s'", contentType)
		}
	})

	// Test custom Content-Type on success response
	t.Run("CustomContentType", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", "/custom-content-type", nil))

		if recorder.Code != http.StatusOK {
			t.Errorf("expected status 200 OK, got %d", recorder.Code)
		}

		// Should have custom Content-Type
		if contentType := recorder.Header().Get("Content-Type"); contentType != "application/vnd.api+json" {
			t.Errorf("expected Content-Type 'application/vnd.api+json', got '%s'", contentType)
		}

		var resp CustomContentTypeResponse
		if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.Message != "Custom content type" {
			t.Errorf("expected message 'Custom content type', got '%s'", resp.Message)
		}
	})

	// Test custom Content-Type on error response
	t.Run("CustomErrorContentType", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", "/custom-error-content-type", nil))

		if recorder.Code != http.StatusBadRequest {
			t.Errorf("expected status 400 Bad Request, got %d", recorder.Code)
		}

		// Should have custom Content-Type
		if contentType := recorder.Header().Get("Content-Type"); contentType != "application/problem+json" {
			t.Errorf("expected Content-Type 'application/problem+json', got '%s'", contentType)
		}

		var errResp CustomContentTypeError
		if err := json.NewDecoder(recorder.Body).Decode(&errResp); err != nil {
			t.Fatalf("failed to decode error response: %v", err)
		}

		if errResp.Message != "Custom error content type" {
			t.Errorf("expected message 'Custom error content type', got '%s'", errResp.Message)
		}
	})
}

type NoBodyError struct {
	_ struct{} `http:"status=204"`
}

func (e *NoBodyError) Error() string {
	return "no body allowed"
}

func TestErrorResponseSkipsBodyWhenNotAllowed(t *testing.T) {
	router := New()

	GET(router, "/no-body-error", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		return nil, &NoBodyError{}
	}, WithErrors(&NoBodyError{}))

	recorder := newBodyTrackingRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/no-body-error", nil))

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", recorder.Code)
	}

	if recorder.wroteBody {
		t.Fatalf("expected no body to be written for 204 responses")
	}

	if recorder.Body.Len() != 0 {
		t.Fatalf("expected empty body, got %q", recorder.Body.String())
	}
}

func TestHeadResponseSkipsBody(t *testing.T) {
	router := New()

	HEAD(router, "/head", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		return &HelloResponse{Message: "should not be sent"}, nil
	})

	recorder := newBodyTrackingRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("HEAD", "/head", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	if recorder.wroteBody {
		t.Fatalf("expected no body to be written for HEAD responses")
	}

	if recorder.Body.Len() != 0 {
		t.Fatalf("expected empty body, got %q", recorder.Body.String())
	}
}

// Test automatic exclusion of routing/metadata fields from JSON
func TestJSONAutoExclusion(t *testing.T) {
	router := New()

	type ResponseWithAllTags struct {
		_           struct{} `http:"status=200"`
		PathField   string   `path:"id"`
		QueryField  string   `query:"page"`
		HeaderField string   `header:"X-Custom"`
		HTTPField   struct{} `http:"status=200"`
		JSONField   string   `json:"data"`
		NormalField string   // No tags
	}

	GET(router, "/test/:id", func(ctx context.Context, req *EmptyRequest) (*ResponseWithAllTags, error) {
		return &ResponseWithAllTags{
			PathField:   "should-not-appear",
			QueryField:  "should-not-appear",
			HeaderField: "header-value",
			JSONField:   "should-appear",
			NormalField: "should-appear-as-NormalField",
		}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/test/123", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	// Verify header was set
	if header := recorder.Header().Get("X-Custom"); header != "header-value" {
		t.Errorf("expected X-Custom header 'header-value', got '%s'", header)
	}

	// Parse JSON response
	var result map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	// Verify excluded fields are NOT in JSON
	if _, exists := result["PathField"]; exists {
		t.Errorf("PathField should be excluded from JSON, but was present")
	}
	if _, exists := result["QueryField"]; exists {
		t.Errorf("QueryField should be excluded from JSON, but was present")
	}
	if _, exists := result["HeaderField"]; exists {
		t.Errorf("HeaderField should be excluded from JSON, but was present")
	}
	if _, exists := result["HTTPField"]; exists {
		t.Errorf("HTTPField should be excluded from JSON, but was present")
	}

	// Verify included fields ARE in JSON
	if data, exists := result["data"]; !exists {
		t.Errorf("'data' should be in JSON")
	} else if data != "should-appear" {
		t.Errorf("expected 'data' to be 'should-appear', got '%v'", data)
	}

	if normalField, exists := result["NormalField"]; !exists {
		t.Errorf("'NormalField' should be in JSON")
	} else if normalField != "should-appear-as-NormalField" {
		t.Errorf("expected 'NormalField' to be 'should-appear-as-NormalField', got '%v'", normalField)
	}
}

// Test JSON exclusion with omitempty
func TestJSONAutoExclusionWithOmitempty(t *testing.T) {
	router := New()

	type ResponseWithOmitempty struct {
		Required    string `json:"required"`
		Optional    string `json:"optional,omitempty"`
		EmptyString string `json:"empty_string,omitempty"`
		HeaderField string `header:"X-Test"`
	}

	GET(router, "/omitempty-test", func(ctx context.Context, req *EmptyRequest) (*ResponseWithOmitempty, error) {
		return &ResponseWithOmitempty{
			Required:    "present",
			Optional:    "also-present",
			EmptyString: "", // Should be omitted
			HeaderField: "test-header",
		}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/omitempty-test", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	// Verify required field is present
	if _, exists := result["required"]; !exists {
		t.Errorf("'required' should be in JSON")
	}

	// Verify optional non-empty field is present
	if _, exists := result["optional"]; !exists {
		t.Errorf("'optional' should be in JSON")
	}

	// Verify empty field with omitempty is NOT present
	if _, exists := result["empty_string"]; exists {
		t.Errorf("'empty_string' should be omitted from JSON due to omitempty")
	}

	// Verify header field is NOT in JSON
	if _, exists := result["HeaderField"]; exists {
		t.Errorf("'HeaderField' should be excluded from JSON")
	}

	// Verify header was set
	if header := recorder.Header().Get("X-Test"); header != "test-header" {
		t.Errorf("expected X-Test header 'test-header', got '%s'", header)
	}
}

// Test JSON exclusion with explicit json:"-" tag
func TestJSONAutoExclusionWithExplicitJsonDash(t *testing.T) {
	router := New()

	type ResponseWithExplicitExclusion struct {
		PublicField  string `json:"public"`
		PrivateField string `json:"-"`        // Explicitly excluded
		HeaderField  string `header:"X-Test"` // Auto-excluded
	}

	GET(router, "/explicit-test", func(ctx context.Context, req *EmptyRequest) (*ResponseWithExplicitExclusion, error) {
		return &ResponseWithExplicitExclusion{
			PublicField:  "visible",
			PrivateField: "invisible",
			HeaderField:  "header-value",
		}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/explicit-test", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	// Verify public field is present
	if val, exists := result["public"]; !exists {
		t.Errorf("'public' should be in JSON")
	} else if val != "visible" {
		t.Errorf("expected 'public' to be 'visible', got '%v'", val)
	}

	// Verify private field with json:"-" is NOT present
	if _, exists := result["PrivateField"]; exists {
		t.Errorf("'PrivateField' should be excluded from JSON due to json:\"-\" tag")
	}

	// Verify header field is NOT present
	if _, exists := result["HeaderField"]; exists {
		t.Errorf("'HeaderField' should be excluded from JSON due to header tag")
	}
}

// Test JSON exclusion in error responses
type ErrorWithMetadata struct {
	_           struct{} `http:"status=400"`
	HeaderField string   `header:"X-Error-Code"`
	ErrorCode   string   `json:"error_code"`
	Message     string   `json:"message"`
}

func (e ErrorWithMetadata) Error() string { return e.Message }

func TestJSONAutoExclusionInErrors(t *testing.T) {
	router := New()

	GET(router, "/error-test", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		return nil, ErrorWithMetadata{
			HeaderField: "BAD_REQUEST",
			ErrorCode:   "invalid_input",
			Message:     "Something went wrong",
		}
	}, WithErrors(ErrorWithMetadata{}))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/error-test", nil))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}

	// Verify header was set
	if header := recorder.Header().Get("X-Error-Code"); header != "BAD_REQUEST" {
		t.Errorf("expected X-Error-Code header 'BAD_REQUEST', got '%s'", header)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	// Verify header field is NOT in JSON
	if _, exists := result["HeaderField"]; exists {
		t.Errorf("'HeaderField' should be excluded from JSON")
	}

	// Verify error fields are present
	if _, exists := result["error_code"]; !exists {
		t.Errorf("'error_code' should be in JSON")
	}
	if _, exists := result["message"]; !exists {
		t.Errorf("'message' should be in JSON")
	}
}

// Test corner case: struct with only routing tags
func TestJSONAutoExclusionAllFieldsExcluded(t *testing.T) {
	router := New()

	type ResponseOnlyRoutingFields struct {
		_           struct{} `http:"status=204"`
		HeaderField string   `header:"X-Custom"`
	}

	GET(router, "/only-routing", func(ctx context.Context, req *EmptyRequest) (*ResponseOnlyRoutingFields, error) {
		return &ResponseOnlyRoutingFields{
			HeaderField: "test",
		}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/only-routing", nil))

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", recorder.Code)
	}

	// Verify header was set
	if header := recorder.Header().Get("X-Custom"); header != "test" {
		t.Errorf("expected X-Custom header 'test', got '%s'", header)
	}

	// 204 responses must not include a body
	if recorder.Body.Len() != 0 {
		t.Fatalf("expected empty body for 204 response, got %q", recorder.Body.String())
	}
}

// Test nested request objects
type Address struct {
	Street  string `json:"street" validate:"required"`
	City    string `json:"city" validate:"required"`
	ZipCode string `json:"zip_code" validate:"required,len=5"`
}

type CreateUserWithAddressRequest struct {
	Name    string  `json:"name" validate:"required,min=3"`
	Email   string  `json:"email" validate:"required,email"`
	Address Address `json:"address" validate:"required"`
}

type CreateUserWithAddressResponse struct {
	ID      int     `json:"id" validate:"required,gt=0"`
	Name    string  `json:"name" validate:"required"`
	Address Address `json:"address" validate:"required"`
}

func TestNestedRequestObjects(t *testing.T) {
	router := New()
	POST(router, "/users", func(ctx context.Context, req *CreateUserWithAddressRequest) (*CreateUserWithAddressResponse, error) {
		return &CreateUserWithAddressResponse{
			ID:      1,
			Name:    req.Name,
			Address: req.Address,
		}, nil
	})

	// Valid nested request
	reqBody := map[string]interface{}{
		"name":  "John Doe",
		"email": "john@example.com",
		"address": map[string]string{
			"street":   "123 Main St",
			"city":     "New York",
			"zip_code": "10001",
		},
	}
	body, _ := json.Marshal(reqBody)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("POST", "/users", bytes.NewReader(body)))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status OK, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var resp CreateUserWithAddressResponse
	if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.ID != 1 {
		t.Errorf("expected ID 1, got %d", resp.ID)
	}
	if resp.Address.City != "New York" {
		t.Errorf("expected City 'New York', got '%s'", resp.Address.City)
	}
}

func TestNestedRequestValidationFailure(t *testing.T) {
	router := New()
	POST(router, "/users", func(ctx context.Context, req *CreateUserWithAddressRequest) (*CreateUserWithAddressResponse, error) {
		return &CreateUserWithAddressResponse{
			ID:      1,
			Name:    req.Name,
			Address: req.Address,
		}, nil
	})

	// Invalid nested request (invalid zip code)
	reqBody := map[string]interface{}{
		"name":  "John Doe",
		"email": "john@example.com",
		"address": map[string]string{
			"street":   "123 Main St",
			"city":     "New York",
			"zip_code": "123", // Invalid: must be 5 digits
		},
	}
	body, _ := json.Marshal(reqBody)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("POST", "/users", bytes.NewReader(body)))

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status BadRequest, got %d", recorder.Code)
	}
}

// Test nested response objects
type ContactInfo struct {
	Email string `json:"email" validate:"required,email"`
	Phone string `json:"phone" validate:"required"`
}

type UserDetailResponse struct {
	_       struct{}    `http:"status=200"`
	UserID  string      `json:"user_id" validate:"required"`
	Name    string      `json:"name" validate:"required"`
	Contact ContactInfo `json:"contact" validate:"required"`
}

func TestNestedResponseObjects(t *testing.T) {
	router := New()
	GET(router, "/users/:id", func(ctx context.Context, req *EmptyRequest) (*UserDetailResponse, error) {
		return &UserDetailResponse{
			UserID: "user-123",
			Name:   "John Doe",
			Contact: ContactInfo{
				Email: "john@example.com",
				Phone: "+1234567890",
			},
		}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/users/123", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var result map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	// Verify nested contact object exists
	contact, ok := result["contact"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'contact' to be an object, got %T", result["contact"])
	}

	// Verify email and phone are present
	if email, exists := contact["email"]; !exists || email != "john@example.com" {
		t.Errorf("expected email 'john@example.com', got '%v'", email)
	}
	if phone, exists := contact["phone"]; !exists || phone != "+1234567890" {
		t.Errorf("expected phone '+1234567890', got '%v'", phone)
	}
}

// Test deeply nested structures
type Metadata struct {
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Version   int    `json:"version"`
}

type FullAddress struct {
	Street   string   `json:"street"`
	City     string   `json:"city"`
	Metadata Metadata `json:"metadata"`
}

type ComplexUserResponse struct {
	ID      int         `json:"id"`
	Name    string      `json:"name"`
	Address FullAddress `json:"address"`
}

func TestDeeplyNestedStructures(t *testing.T) {
	router := New()
	GET(router, "/complex", func(ctx context.Context, req *EmptyRequest) (*ComplexUserResponse, error) {
		return &ComplexUserResponse{
			ID:   1,
			Name: "Test User",
			Address: FullAddress{
				Street: "123 Main St",
				City:   "New York",
				Metadata: Metadata{
					CreatedAt: "2024-01-01",
					UpdatedAt: "2024-01-02",
					Version:   1,
				},
			},
		}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/complex", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var result map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	// Navigate to deeply nested metadata
	address, ok := result["address"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'address' to be an object")
	}

	metadata, ok := address["metadata"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'metadata' to be an object")
	}

	// Verify nested fields are present and correct
	if createdAt, exists := metadata["created_at"]; !exists || createdAt != "2024-01-01" {
		t.Errorf("expected created_at '2024-01-01', got '%v'", createdAt)
	}
	if version, exists := metadata["version"]; !exists || version != float64(1) {
		t.Errorf("expected version 1, got '%v'", version)
	}
}

// Test arrays of nested objects
type Item struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type ItemListResponse struct {
	Items []Item `json:"items"`
	Count int    `json:"count"`
}

func TestArrayOfNestedObjects(t *testing.T) {
	router := New()
	GET(router, "/items", func(ctx context.Context, req *EmptyRequest) (*ItemListResponse, error) {
		return &ItemListResponse{
			Items: []Item{
				{ID: 1, Name: "Item 1"},
				{ID: 2, Name: "Item 2"},
			},
			Count: 2,
		}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/items", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var result map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	items, ok := result["items"].([]interface{})
	if !ok {
		t.Fatalf("expected 'items' to be an array")
	}

	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}

	// Check first item
	item1, ok := items[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected item to be an object")
	}

	if id, exists := item1["id"]; !exists || id != float64(1) {
		t.Errorf("expected id 1, got '%v'", id)
	}
	if name, exists := item1["name"]; !exists || name != "Item 1" {
		t.Errorf("expected name 'Item 1', got '%v'", name)
	}
}

// Test nested error objects
type ErrorDetails struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type DetailedError struct {
	_       struct{}     `http:"status=400"`
	Type    string       `json:"type"`
	Details ErrorDetails `json:"details"`
}

func (e DetailedError) Error() string { return e.Type }

func TestNestedErrorObjects(t *testing.T) {
	router := New()
	POST(router, "/validate", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		return nil, DetailedError{
			Type: "validation_error",
			Details: ErrorDetails{
				Field:   "email",
				Message: "invalid email format",
			},
		}
	}, WithErrors(DetailedError{}))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("POST", "/validate", nil))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	// Verify type field is present
	if typ, exists := result["type"]; !exists || typ != "validation_error" {
		t.Errorf("expected type 'validation_error', got '%v'", typ)
	}

	// Verify nested details object
	details, ok := result["details"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'details' to be an object")
	}

	if field, exists := details["field"]; !exists || field != "email" {
		t.Errorf("expected field 'email', got '%v'", field)
	}
	if message, exists := details["message"]; !exists || message != "invalid email format" {
		t.Errorf("expected message 'invalid email format', got '%v'", message)
	}
}

// Test custom error handler functionality
func TestCustomErrorHandler(t *testing.T) {
	var capturedError error
	var capturedWriter http.ResponseWriter
	var capturedRequest *http.Request

	config := &Config{
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			capturedError = err
			capturedWriter = w
			capturedRequest = r

			// Return custom JSON error response
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTeapot) // Use 418 to distinguish from default
			json.NewEncoder(w).Encode(map[string]string{
				"custom_error": "true",
				"message":      err.Error(),
			})
		},
	}

	router := NewWithConfig(config)

	// Test handler that triggers validation error
	POST(router, "/test", func(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
		return &CreateUserResponse{
			ID:    1,
			Name:  req.Name,
			Email: req.Email,
		}, nil
	})

	// Invalid request (name too short) - should trigger validation error
	reqBody := CreateUserRequest{
		Name:  "Jo",
		Email: "john@example.com",
	}
	body, _ := json.Marshal(reqBody)

	recorder := httptest.NewRecorder()
	httpReq := httptest.NewRequest("POST", "/test", bytes.NewReader(body))
	router.ServeHTTP(recorder, httpReq)

	// Verify custom error handler was called
	if capturedError == nil {
		t.Fatal("expected error handler to be called")
	}

	if capturedWriter == nil {
		t.Error("expected ResponseWriter to be passed to error handler")
	}

	if capturedRequest == nil {
		t.Error("expected Request to be passed to error handler")
	}

	// Verify custom status code
	if recorder.Code != http.StatusTeapot {
		t.Errorf("expected status 418 (custom), got %d", recorder.Code)
	}

	// Verify custom response body
	var resp map[string]string
	if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["custom_error"] != "true" {
		t.Errorf("expected custom_error 'true', got '%s'", resp["custom_error"])
	}
}

// Test error kinds with custom handler
func TestCustomErrorHandlerWithErrorKinds(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(*Sprout)
		request       func() *http.Request
		expectedKind  ErrorKind
		expectedError string
	}{
		{
			name: "ParseError",
			setup: func(s *Sprout) {
				GET(s, "/users/:id", func(ctx context.Context, req *GetUserRequest) (*GetUserResponse, error) {
					return &GetUserResponse{
						UserID:    req.UserID,
						Page:      req.Page,
						Limit:     req.Limit,
						AuthToken: req.AuthToken,
					}, nil
				})
			},
			request: func() *http.Request {
				// Invalid query param (page should be int)
				return httptest.NewRequest("GET", "/users/123?page=invalid&limit=10", nil)
			},
			expectedKind:  ErrorKindParse,
			expectedError: "invalid query parameter 'page'",
		},
		{
			name: "ValidationError",
			setup: func(s *Sprout) {
				POST(s, "/users", func(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
					return &CreateUserResponse{
						ID:    1,
						Name:  req.Name,
						Email: req.Email,
					}, nil
				})
			},
			request: func() *http.Request {
				// Invalid body (name too short)
				reqBody := CreateUserRequest{
					Name:  "Jo",
					Email: "john@example.com",
				}
				body, _ := json.Marshal(reqBody)
				return httptest.NewRequest("POST", "/users", bytes.NewReader(body))
			},
			expectedKind:  ErrorKindValidation,
			expectedError: "request validation failed",
		},
		{
			name: "ResponseValidationError",
			setup: func(s *Sprout) {
				GET(s, "/invalid-response", func(ctx context.Context, req *EmptyRequest) (*CreateUserResponse, error) {
					// Return response with invalid ID (must be > 0)
					return &CreateUserResponse{
						ID:    -1, // Invalid!
						Name:  "Test",
						Email: "test@example.com",
					}, nil
				})
			},
			request: func() *http.Request {
				return httptest.NewRequest("GET", "/invalid-response", nil)
			},
			expectedKind:  ErrorKindResponseValidation,
			expectedError: "response validation failed",
		},
		{
			name: "ErrorValidationError",
			setup: func(s *Sprout) {
				GET(s, "/invalid-error", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
					// Return error with missing required field
					return nil, NotFoundError{
						Resource: "user",
						Message:  "", // Invalid! Message is required
					}
				}, WithErrors(NotFoundError{}))
			},
			request: func() *http.Request {
				return httptest.NewRequest("GET", "/invalid-error", nil)
			},
			expectedKind:  ErrorKindErrorValidation,
			expectedError: "error response validation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedError error

			config := &Config{
				ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
					capturedError = err
					w.WriteHeader(http.StatusTeapot)
				},
			}

			router := NewWithConfig(config)
			tt.setup(router)

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, tt.request())

			if capturedError == nil {
				t.Fatal("expected error handler to be called")
			}

			// Extract Error using errors.As
			var sproutErr *Error
			if !errors.As(capturedError, &sproutErr) {
				t.Fatalf("expected *Error, got %T", capturedError)
			}

			if sproutErr.Kind != tt.expectedKind {
				t.Errorf("expected kind %s, got %s", tt.expectedKind, sproutErr.Kind)
			}

			if !bytes.Contains([]byte(sproutErr.Message), []byte(tt.expectedError)) {
				t.Errorf("expected error message to contain '%s', got '%s'", tt.expectedError, sproutErr.Message)
			}

			// Verify custom status code was used
			if recorder.Code != http.StatusTeapot {
				t.Errorf("expected status 418 (custom handler), got %d", recorder.Code)
			}
		})
	}
}

// Test default error handling (no custom handler)
func TestDefaultErrorHandling(t *testing.T) {
	router := New() // No custom config

	// Test handler that triggers validation error
	POST(router, "/test", func(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
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
	router.ServeHTTP(recorder, httptest.NewRequest("POST", "/test", bytes.NewReader(body)))

	// Default handler should return 400 for validation errors
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", recorder.Code)
	}

	// Default handler returns plain text error
	if !bytes.Contains(recorder.Body.Bytes(), []byte("validation_error")) {
		t.Logf("Response body: %s", recorder.Body.String())
	}
}

// Test unwrapping Error
func TestErrorUnwrap(t *testing.T) {
	underlyingErr := errors.New("underlying error")
	sproutErr := &Error{
		Kind:    ErrorKindParse,
		Message: "parse failed",
		Err:     underlyingErr,
	}

	unwrapped := sproutErr.Unwrap()
	if unwrapped != underlyingErr {
		t.Errorf("expected unwrapped error to be %v, got %v", underlyingErr, unwrapped)
	}
}

// Test Error string formatting
func TestErrorString(t *testing.T) {
	tests := []struct {
		name     string
		err      *Error
		expected string
	}{
		{
			name: "WithUnderlyingError",
			err: &Error{
				Kind:    ErrorKindValidation,
				Message: "validation failed",
				Err:     errors.New("field 'name' is required"),
			},
			expected: "validation_error: validation failed: field 'name' is required",
		},
		{
			name: "WithoutUnderlyingError",
			err: &Error{
				Kind:    ErrorKindParse,
				Message: "parse failed",
			},
			expected: "parse_error: parse failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.err.Error()
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

// Test strict error type checking (default behavior)
func TestStrictErrorTypesDefault(t *testing.T) {
	router := New() // Default: strict = true

	// Handler that returns undeclared error type
	POST(router, "/test", func(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
		if req.Name == "trigger" {
			// Return NotFoundError, but only ConflictError is declared
			return nil, NotFoundError{Resource: "user", Message: "user not found"}
		}
		return &CreateUserResponse{ID: 1, Name: req.Name, Email: req.Email}, nil
	}, WithErrors(ConflictError{})) // Only ConflictError declared, NOT NotFoundError

	reqBody := CreateUserRequest{
		Name:  "trigger",
		Email: "test@example.com",
	}
	body, _ := json.Marshal(reqBody)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("POST", "/test", bytes.NewReader(body)))

	// Should return 500 because error type not declared and strict mode is on
	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500 (strict mode), got %d", recorder.Code)
	}

	if !bytes.Contains(recorder.Body.Bytes(), []byte("undeclared_error_type")) {
		t.Errorf("expected 'undeclared_error_type' in response, got: %s", recorder.Body.String())
	}
}

// Test strict error type checking disabled
func TestStrictErrorTypesDisabled(t *testing.T) {
	falseVal := false
	config := &Config{
		StrictErrorTypes: &falseVal,
	}
	router := NewWithConfig(config)

	// Handler that returns undeclared error type
	POST(router, "/test", func(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
		if req.Name == "trigger" {
			// Return NotFoundError, but only ConflictError is declared
			return nil, NotFoundError{Resource: "user", Message: "user not found"}
		}
		return &CreateUserResponse{ID: 1, Name: req.Name, Email: req.Email}, nil
	}, WithErrors(ConflictError{})) // Only ConflictError declared

	reqBody := CreateUserRequest{
		Name:  "trigger",
		Email: "test@example.com",
	}
	body, _ := json.Marshal(reqBody)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("POST", "/test", bytes.NewReader(body)))

	// Should return 404 (error's status code) because strict mode is off
	// The undeclared error is allowed with just a warning
	if recorder.Code != http.StatusNotFound {
		t.Errorf("expected status 404 (error's status), got %d: %s", recorder.Code, recorder.Body.String())
	}

	// Should still get valid error response
	var errResp NotFoundError
	if err := json.NewDecoder(recorder.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	if errResp.Resource != "user" {
		t.Errorf("expected resource 'user', got '%s'", errResp.Resource)
	}
}

// Test declared errors work in strict mode
func TestStrictErrorTypesDeclared(t *testing.T) {
	router := New() // Default: strict = true

	// Handler with properly declared error types
	POST(router, "/test", func(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
		if req.Name == "notfound" {
			return nil, NotFoundError{Resource: "user", Message: "user not found"}
		}
		if req.Name == "conflict" {
			return nil, ConflictError{Field: "email", Message: "email exists"}
		}
		return &CreateUserResponse{ID: 1, Name: req.Name, Email: req.Email}, nil
	}, WithErrors(NotFoundError{}, ConflictError{})) // Both error types declared

	// Test NotFoundError (declared)
	t.Run("NotFound", func(t *testing.T) {
		reqBody := CreateUserRequest{
			Name:  "notfound",
			Email: "test@example.com",
		}
		body, _ := json.Marshal(reqBody)

		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("POST", "/test", bytes.NewReader(body)))

		if recorder.Code != http.StatusNotFound {
			t.Errorf("expected status 404, got %d: %s", recorder.Code, recorder.Body.String())
		}

		var errResp NotFoundError
		if err := json.NewDecoder(recorder.Body).Decode(&errResp); err != nil {
			t.Fatalf("failed to decode error response: %v", err)
		}

		if errResp.Resource != "user" {
			t.Errorf("expected resource 'user', got '%s'", errResp.Resource)
		}
	})

	// Test ConflictError (declared)
	t.Run("Conflict", func(t *testing.T) {
		reqBody := CreateUserRequest{
			Name:  "conflict",
			Email: "test@example.com",
		}
		body, _ := json.Marshal(reqBody)

		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("POST", "/test", bytes.NewReader(body)))

		if recorder.Code != http.StatusConflict {
			t.Errorf("expected status 409, got %d: %s", recorder.Code, recorder.Body.String())
		}

		var errResp ConflictError
		if err := json.NewDecoder(recorder.Body).Decode(&errResp); err != nil {
			t.Fatalf("failed to decode error response: %v", err)
		}

		if errResp.Field != "email" {
			t.Errorf("expected field 'email', got '%s'", errResp.Field)
		}
	})
}

// Test explicitly enabling strict error types
func TestStrictErrorTypesExplicitlyEnabled(t *testing.T) {
	trueVal := true
	config := &Config{
		StrictErrorTypes: &trueVal,
	}
	router := NewWithConfig(config)

	// Handler that returns undeclared error type
	POST(router, "/test", func(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
		if req.Name == "trigger" {
			return nil, NotFoundError{Resource: "user", Message: "user not found"}
		}
		return &CreateUserResponse{ID: 1, Name: req.Name, Email: req.Email}, nil
	}, WithErrors(ConflictError{})) // Only ConflictError declared

	reqBody := CreateUserRequest{
		Name:  "trigger",
		Email: "test@example.com",
	}
	body, _ := json.Marshal(reqBody)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("POST", "/test", bytes.NewReader(body)))

	// Should return 500 because error type not declared and strict mode is explicitly on
	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500 (strict mode), got %d", recorder.Code)
	}

	if !bytes.Contains(recorder.Body.Bytes(), []byte("undeclared_error_type")) {
		t.Errorf("expected 'undeclared_error_type' in response, got: %s", recorder.Body.String())
	}
}

// Test custom error handler can intercept undeclared error types
func TestStrictErrorTypesCustomHandler(t *testing.T) {
	var capturedErrorKind ErrorKind

	config := &Config{
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			// Extract ErrorKind
			var sproutErr *Error
			if errors.As(err, &sproutErr) {
				capturedErrorKind = sproutErr.Kind
			}

			// Custom handling for undeclared errors
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway) // Use 502 to distinguish custom handling
			json.NewEncoder(w).Encode(map[string]string{
				"custom_handling": "true",
				"error_kind":      string(capturedErrorKind),
			})
		},
	}

	router := NewWithConfig(config)

	// Handler that returns undeclared error type
	POST(router, "/test", func(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
		if req.Name == "trigger" {
			return nil, NotFoundError{Resource: "user", Message: "user not found"}
		}
		return &CreateUserResponse{ID: 1, Name: req.Name, Email: req.Email}, nil
	}, WithErrors(ConflictError{})) // Only ConflictError declared

	reqBody := CreateUserRequest{
		Name:  "trigger",
		Email: "test@example.com",
	}
	body, _ := json.Marshal(reqBody)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("POST", "/test", bytes.NewReader(body)))

	// Verify custom handler was invoked
	if recorder.Code != http.StatusBadGateway {
		t.Errorf("expected status 502 (custom handler), got %d", recorder.Code)
	}

	// Verify error kind was captured
	if capturedErrorKind != ErrorKindUndeclaredError {
		t.Errorf("expected ErrorKindUndeclaredError, got %s", capturedErrorKind)
	}

	// Verify custom response body
	var resp map[string]string
	if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["custom_handling"] != "true" {
		t.Errorf("expected custom_handling 'true', got '%s'", resp["custom_handling"])
	}

	if resp["error_kind"] != "undeclared_error_type" {
		t.Errorf("expected error_kind 'undeclared_error_type', got '%s'", resp["error_kind"])
	}
}

// Test base path functionality
func TestBasePath(t *testing.T) {
	config := &Config{
		BasePath: "/api/v1",
	}
	router := NewWithConfig(config)

	POST(router, "/users", func(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
		return &CreateUserResponse{
			ID:    123,
			Name:  req.Name,
			Email: req.Email,
		}, nil
	})

	// Request should be made to /api/v1/users, not /users
	reqBody := CreateUserRequest{
		Name:  "John Doe",
		Email: "john@example.com",
	}
	body, _ := json.Marshal(reqBody)

	// Request to base path should work
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("POST", "/api/v1/users", bytes.NewReader(body)))

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status OK for /api/v1/users, got %d: %s", recorder.Code, recorder.Body.String())
	}

	// Request to route without base path should NOT work
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("POST", "/users", bytes.NewReader(body)))

	if recorder.Code != http.StatusNotFound {
		t.Errorf("expected status 404 for /users (no base path), got %d", recorder.Code)
	}
}

// Test base path with trailing slash
func TestBasePathWithTrailingSlash(t *testing.T) {
	config := &Config{
		BasePath: "/api/v1/", // Trailing slash should be handled
	}
	router := NewWithConfig(config)

	GET(router, "/users", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		return &HelloResponse{Message: "success"}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/api/v1/users", nil))

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status OK, got %d", recorder.Code)
	}
}

// Test base path without leading slash
func TestBasePathWithoutLeadingSlash(t *testing.T) {
	config := &Config{
		BasePath: "api/v1", // Missing leading slash should be handled
	}
	router := NewWithConfig(config)

	GET(router, "/users", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		return &HelloResponse{Message: "success"}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/api/v1/users", nil))

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status OK, got %d", recorder.Code)
	}
}

// Test empty base path
func TestEmptyBasePath(t *testing.T) {
	config := &Config{
		BasePath: "", // Empty base path should work like New()
	}
	router := NewWithConfig(config)

	GET(router, "/users", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		return &HelloResponse{Message: "success"}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/users", nil))

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status OK, got %d", recorder.Code)
	}
}

// Test base path with path parameters
func TestBasePathWithPathParams(t *testing.T) {
	config := &Config{
		BasePath: "/api/v1",
	}
	router := NewWithConfig(config)

	type GetUserByIDRequest struct {
		UserID string `path:"id" validate:"required"`
	}

	GET(router, "/users/:id", func(ctx context.Context, req *GetUserByIDRequest) (*HelloResponse, error) {
		return &HelloResponse{Message: "User ID: " + req.UserID}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/api/v1/users/123", nil))

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status OK, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var resp HelloResponse
	if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Message != "User ID: 123" {
		t.Errorf("expected message 'User ID: 123', got '%s'", resp.Message)
	}
}

// Test multiple routes with base path
func TestMultipleRoutesWithBasePath(t *testing.T) {
	config := &Config{
		BasePath: "/api/v1",
	}
	router := NewWithConfig(config)

	GET(router, "/users", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		return &HelloResponse{Message: "users"}, nil
	})

	POST(router, "/users", func(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
		return &CreateUserResponse{
			ID:    1,
			Name:  req.Name,
			Email: req.Email,
		}, nil
	})

	GET(router, "/items", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		return &HelloResponse{Message: "items"}, nil
	})

	// Test GET /api/v1/users
	t.Run("GET /users", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", "/api/v1/users", nil))

		if recorder.Code != http.StatusOK {
			t.Errorf("expected status OK, got %d", recorder.Code)
		}

		var resp HelloResponse
		json.NewDecoder(recorder.Body).Decode(&resp)
		if resp.Message != "users" {
			t.Errorf("expected 'users', got '%s'", resp.Message)
		}
	})

	// Test POST /api/v1/users
	t.Run("POST /users", func(t *testing.T) {
		reqBody := CreateUserRequest{Name: "John", Email: "john@example.com"}
		body, _ := json.Marshal(reqBody)

		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("POST", "/api/v1/users", bytes.NewReader(body)))

		if recorder.Code != http.StatusOK {
			t.Errorf("expected status OK, got %d", recorder.Code)
		}
	})

	// Test GET /api/v1/items
	t.Run("GET /items", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", "/api/v1/items", nil))

		if recorder.Code != http.StatusOK {
			t.Errorf("expected status OK, got %d", recorder.Code)
		}

		var resp HelloResponse
		json.NewDecoder(recorder.Body).Decode(&resp)
		if resp.Message != "items" {
			t.Errorf("expected 'items', got '%s'", resp.Message)
		}
	})
}

func TestNestedRouterMountsPrefix(t *testing.T) {
	router := New()
	auth := router.Mount("/auth", nil)

	GET(auth, "/login", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		return &HelloResponse{Message: "auth-login"}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/auth/login", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status OK, got %d", recorder.Code)
	}

	var resp HelloResponse
	if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Message != "auth-login" {
		t.Errorf("expected message 'auth-login', got '%s'", resp.Message)
	}

	// Without prefix the route should not be found.
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/login", nil))

	if recorder.Code != http.StatusNotFound {
		t.Errorf("expected status 404 for missing prefix, got %d", recorder.Code)
	}
}

func TestNestedRouterWithParentBasePath(t *testing.T) {
	router := NewWithConfig(&Config{
		BasePath: "/api",
	})

	auth := router.Mount("/auth", nil)

	GET(auth, "/login", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		return &HelloResponse{Message: "auth-login"}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/api/auth/login", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status OK, got %d", recorder.Code)
	}

	var resp HelloResponse
	if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Message != "auth-login" {
		t.Errorf("expected message 'auth-login', got '%s'", resp.Message)
	}

	// Requests missing either prefix should be 404.
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/auth/login", nil))

	if recorder.Code != http.StatusNotFound {
		t.Errorf("expected status 404 without base path, got %d", recorder.Code)
	}
}

func TestNestedRouterInheritsErrorHandler(t *testing.T) {
	var handled bool

	router := NewWithConfig(&Config{
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			handled = true
			w.WriteHeader(599)
			w.Write([]byte("custom error"))
		},
	})

	auth := router.Mount("/auth", nil)

	type AuthRequest struct {
		Token string `header:"Authorization" validate:"required"`
	}

	GET(auth, "/login", func(ctx context.Context, req *AuthRequest) (*HelloResponse, error) {
		return &HelloResponse{Message: "should not reach"}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/auth/login", nil))

	if !handled {
		t.Fatalf("expected parent error handler to be invoked")
	}

	if recorder.Code != 599 {
		t.Fatalf("expected status 599 from custom error handler, got %d", recorder.Code)
	}

	if !bytes.Contains(recorder.Body.Bytes(), []byte("custom error")) {
		t.Errorf("expected custom error body, got %s", recorder.Body.String())
	}
}

func TestNestedRouterOverridesErrorHandler(t *testing.T) {
	var parentCalled bool
	var childCalled bool

	router := NewWithConfig(&Config{
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			parentCalled = true
			w.WriteHeader(597)
			w.Write([]byte("parent error"))
		},
	})

	child := router.Mount("/child", &Config{
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			childCalled = true
			w.WriteHeader(598)
			w.Write([]byte("child error"))
		},
	})

	type ChildRequest struct {
		Token string `header:"Authorization" validate:"required"`
	}

	GET(child, "/secure", func(ctx context.Context, req *ChildRequest) (*HelloResponse, error) {
		return &HelloResponse{Message: "should not reach"}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/child/secure", nil))

	if parentCalled {
		t.Fatalf("expected parent error handler to be skipped")
	}

	if !childCalled {
		t.Fatalf("expected child error handler to be invoked")
	}

	if recorder.Code != 598 {
		t.Fatalf("expected status 598 from child error handler, got %d", recorder.Code)
	}
}

func TestNestedRouterOverridesStrictFlag(t *testing.T) {
	router := New()

	strictFalse := false
	child := router.Mount("/loose", &Config{
		StrictErrorTypes: &strictFalse,
	})

	GET(child, "/test", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		return nil, &TeapotError{Msg: "teapot"}
	}, WithErrors(&Error{}))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/loose/test", nil))

	if recorder.Code != 418 {
		t.Fatalf("expected status 418 from custom error, got %d", recorder.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["message"] != "teapot" {
		t.Errorf("expected message 'teapot', got %s", resp["message"])
	}
}

// Test 404 Not Found with default error handler
func TestNotFoundDefaultHandler(t *testing.T) {
	router := New()

	GET(router, "/users", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		return &HelloResponse{Message: "users"}, nil
	})

	// Request to non-existent route
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/nonexistent", nil))

	if recorder.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", recorder.Code)
	}

	// Should contain error message
	if !bytes.Contains(recorder.Body.Bytes(), []byte("not_found")) {
		t.Errorf("expected 'not_found' in response, got: %s", recorder.Body.String())
	}

	if !bytes.Contains(recorder.Body.Bytes(), []byte("GET /nonexistent")) {
		t.Errorf("expected route info in response, got: %s", recorder.Body.String())
	}
}

// Test 405 Method Not Allowed with default error handler
func TestMethodNotAllowedDefaultHandler(t *testing.T) {
	router := New()
	router.HandleMethodNotAllowed = true // Enable 405 responses

	GET(router, "/users", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		return &HelloResponse{Message: "users"}, nil
	})

	// Request with wrong method
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("POST", "/users", nil))

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", recorder.Code)
	}

	// Should contain error message
	if !bytes.Contains(recorder.Body.Bytes(), []byte("method_not_allowed")) {
		t.Errorf("expected 'method_not_allowed' in response, got: %s", recorder.Body.String())
	}
}

// Test 404 with custom error handler
func TestNotFoundCustomHandler(t *testing.T) {
	var capturedKind ErrorKind

	config := &Config{
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			var sproutErr *Error
			if errors.As(err, &sproutErr) {
				capturedKind = sproutErr.Kind

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				json.NewEncoder(w).Encode(map[string]string{
					"error":   "custom_not_found",
					"message": sproutErr.Message,
					"path":    r.URL.Path,
				})
			}
		},
	}

	router := NewWithConfig(config)

	GET(router, "/users", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		return &HelloResponse{Message: "users"}, nil
	})

	// Request to non-existent route
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/nonexistent", nil))

	// Verify error kind was captured
	if capturedKind != ErrorKindNotFound {
		t.Errorf("expected ErrorKindNotFound, got %s", capturedKind)
	}

	// Verify custom response
	if recorder.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", recorder.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["error"] != "custom_not_found" {
		t.Errorf("expected 'custom_not_found', got '%s'", resp["error"])
	}

	if resp["path"] != "/nonexistent" {
		t.Errorf("expected path '/nonexistent', got '%s'", resp["path"])
	}
}

// Test 405 with custom error handler
func TestMethodNotAllowedCustomHandler(t *testing.T) {
	var capturedKind ErrorKind

	config := &Config{
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			var sproutErr *Error
			if errors.As(err, &sproutErr) {
				capturedKind = sproutErr.Kind

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusMethodNotAllowed)
				json.NewEncoder(w).Encode(map[string]string{
					"error":  "custom_method_not_allowed",
					"method": r.Method,
					"path":   r.URL.Path,
				})
			}
		},
	}

	router := NewWithConfig(config)
	router.HandleMethodNotAllowed = true // Enable 405 responses

	GET(router, "/users", func(ctx context.Context, req *EmptyRequest) (*HelloResponse, error) {
		return &HelloResponse{Message: "users"}, nil
	})

	// Request with wrong method
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("POST", "/users", nil))

	// Verify error kind was captured
	if capturedKind != ErrorKindMethodNotAllowed {
		t.Errorf("expected ErrorKindMethodNotAllowed, got %s", capturedKind)
	}

	// Verify custom response
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", recorder.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["error"] != "custom_method_not_allowed" {
		t.Errorf("expected 'custom_method_not_allowed', got '%s'", resp["error"])
	}

	if resp["method"] != "POST" {
		t.Errorf("expected method 'POST', got '%s'", resp["method"])
	}
}

// Test that all error kinds go through same handler
func TestConsistentErrorHandling(t *testing.T) {
	errorKinds := []ErrorKind{}

	config := &Config{
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			var sproutErr *Error
			if errors.As(err, &sproutErr) {
				errorKinds = append(errorKinds, sproutErr.Kind)

				w.Header().Set("Content-Type", "application/json")
				status := http.StatusInternalServerError
				switch sproutErr.Kind {
				case ErrorKindParse, ErrorKindValidation:
					status = http.StatusBadRequest
				case ErrorKindNotFound:
					status = http.StatusNotFound
				case ErrorKindMethodNotAllowed:
					status = http.StatusMethodNotAllowed
				}
				w.WriteHeader(status)
				json.NewEncoder(w).Encode(map[string]string{
					"kind":    string(sproutErr.Kind),
					"message": sproutErr.Message,
				})
			}
		},
	}

	router := NewWithConfig(config)
	router.HandleMethodNotAllowed = true

	type BadRequest struct {
		Page int `query:"page" validate:"required,gte=1"`
	}

	GET(router, "/test", func(ctx context.Context, req *BadRequest) (*HelloResponse, error) {
		return &HelloResponse{Message: "ok"}, nil
	})

	// Test 404 - goes through ErrorHandler
	t.Run("404 NotFound", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", "/nonexistent", nil))

		if recorder.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", recorder.Code)
		}

		if recorder.Header().Get("Content-Type") != "application/json" {
			t.Errorf("expected JSON content type")
		}
	})

	// Test 405 - goes through ErrorHandler
	t.Run("405 MethodNotAllowed", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("POST", "/test", nil))

		if recorder.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", recorder.Code)
		}

		if recorder.Header().Get("Content-Type") != "application/json" {
			t.Errorf("expected JSON content type")
		}
	})

	// Test 400 Validation - goes through ErrorHandler
	t.Run("400 Validation", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", "/test", nil))

		if recorder.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", recorder.Code)
		}

		if recorder.Header().Get("Content-Type") != "application/json" {
			t.Errorf("expected JSON content type")
		}
	})

	// Verify all went through the same handler
	if len(errorKinds) != 3 {
		t.Errorf("expected 3 errors captured, got %d", len(errorKinds))
	}

	expectedKinds := map[ErrorKind]bool{
		ErrorKindNotFound:         true,
		ErrorKindMethodNotAllowed: true,
		ErrorKindValidation:       true,
	}

	for _, kind := range errorKinds {
		if !expectedKinds[kind] {
			t.Errorf("unexpected error kind: %s", kind)
		}
	}
}

// Test nil response handling with empty struct
func TestNilResponseWithEmptyStruct(t *testing.T) {
	router := New()

	// Empty response type with no required fields
	type EmptyResponse struct{}

	// Handler returns nil, should be converted to empty struct and serialized as {}
	DELETE(router, "/users/:id", func(ctx context.Context, req *EmptyRequest) (*EmptyResponse, error) {
		return nil, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("DELETE", "/users/123", nil))

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	// Should serialize as empty JSON object {}
	var result map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected empty JSON object {}, got %v", result)
	}
}

// Test nil response with 204 No Content
func TestNilResponseWithNoContent(t *testing.T) {
	router := New()

	// Empty response type with 204 status
	type NoContentResponse struct {
		_ struct{} `http:"status=204"`
	}

	// Handler returns nil, should serialize to {} with 204 status
	DELETE(router, "/items/:id", func(ctx context.Context, req *EmptyRequest) (*NoContentResponse, error) {
		return nil, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("DELETE", "/items/456", nil))

	if recorder.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d: %s", recorder.Code, recorder.Body.String())
	}

	// 204 responses must not include a body
	if recorder.Body.Len() != 0 {
		t.Fatalf("expected empty body for 204 response, got %q", recorder.Body.String())
	}
}

// Test nil response fails validation when response has required fields
func TestNilResponseWithRequiredFields(t *testing.T) {
	router := New()

	// Response type with required field
	type UserResponse struct {
		ID int `json:"id" validate:"required,gt=0"`
	}

	// Handler returns nil, but response type has required fields
	GET(router, "/users/:id", func(ctx context.Context, req *EmptyRequest) (*UserResponse, error) {
		return nil, nil // This should fail validation!
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/users/123", nil))

	// Should return 500 because validation failed
	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500 (validation failed), got %d: %s", recorder.Code, recorder.Body.String())
	}

	// Should contain validation error message
	if !bytes.Contains(recorder.Body.Bytes(), []byte("response validation failed")) {
		t.Errorf("expected validation error message, got: %s", recorder.Body.String())
	}
}

// Test nil response works with optional fields (omitempty)
func TestNilResponseWithOptionalFields(t *testing.T) {
	router := New()

	// Response type with only optional fields
	type OptionalResponse struct {
		Name  string `json:"name,omitempty"`
		Email string `json:"email,omitempty"`
	}

	// Handler returns nil, all fields are optional so it should work
	GET(router, "/optional", func(ctx context.Context, req *EmptyRequest) (*OptionalResponse, error) {
		return nil, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/optional", nil))

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	// Should serialize as empty JSON object {} (omitempty skips zero values)
	var result map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected empty JSON object {}, got %v", result)
	}
}
