package sprout

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

// Test error handling with HTTPError interface

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
	_       struct{}  `http:"status=400"`
	Fields  []string `json:"fields" validate:"required,min=1"`
	Message string   `json:"message" validate:"required"`
}

func (e ValidationError) Error() string {
	return e.Message
}

func TestSproutHTTPError(t *testing.T) {
	router := New()

	// Register handler with expected errors
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

	if !bytes.Contains(recorder.Body.Bytes(), []byte("Error response validation failed")) {
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

// Test automatic exclusion of routing/metadata fields from JSON
func TestJSONAutoExclusion(t *testing.T) {
	router := New()

	type ResponseWithAllTags struct {
		_            struct{} `http:"status=200"`
		PathField    string   `path:"id"`
		QueryField   string   `query:"page"`
		HeaderField  string   `header:"X-Custom"`
		HTTPField    struct{} `http:"status=200"`
		JSONField    string   `json:"data"`
		NormalField  string   // No tags
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

	// Should return empty JSON object {}
	var result map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected empty JSON object {}, got %v", result)
	}
}
