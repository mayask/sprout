package sprout_test

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/mayask/sprout"
)

// Example 1: Simple POST with body validation
type CreateUserRequest struct {
	Name  string `json:"name" validate:"required,min=3,max=50"`
	Email string `json:"email" validate:"required,email"`
	Age   int    `json:"age" validate:"required,gte=18,lte=120"`
}

type CreateUserResponse struct {
	ID      int    `json:"id" validate:"required,gt=0"`
	Name    string `json:"name" validate:"required"`
	Email   string `json:"email" validate:"required,email"`
	Message string `json:"message" validate:"required"`
}

// Example 2: GET with path params, query params, and headers
type GetUserRequest struct {
	// Path parameter from URL /users/:id
	UserID string `path:"id" validate:"required,uuid4"`

	// Query parameters from ?page=1&limit=10
	Page  int `query:"page" validate:"omitempty,gte=1"`
	Limit int `query:"limit" validate:"omitempty,gte=1,lte=100"`

	// Header validation
	AuthToken string `header:"Authorization" validate:"required"`
}

type GetUserResponse struct {
	UserID string `json:"user_id" validate:"required"`
	Name   string `json:"name" validate:"required"`
	Email  string `json:"email" validate:"required,email"`
}

// Example 3: PUT combining path params, headers, and body
type UpdateUserRequest struct {
	// Path parameter
	UserID string `path:"id" validate:"required"`

	// Header
	AuthToken string `header:"Authorization" validate:"required"`

	// JSON body fields
	Name  string `json:"name" validate:"required,min=3"`
	Email string `json:"email" validate:"required,email"`
}

type UpdateUserResponse struct {
	UserID  string `json:"user_id" validate:"required"`
	Name    string `json:"name" validate:"required"`
	Email   string `json:"email" validate:"required"`
	Message string `json:"message" validate:"required"`
}

func Example() {
	router := sprout.New()

	// POST /users - Create user with body validation
	sprout.POST(router, "/users", func(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
		return &CreateUserResponse{
			ID:      123,
			Name:    req.Name,
			Email:   req.Email,
			Message: fmt.Sprintf("User %s created successfully", req.Name),
		}, nil
	})

	// GET /users/:id - Get user with path, query, and header validation
	sprout.GET(router, "/users/:id", func(ctx context.Context, req *GetUserRequest) (*GetUserResponse, error) {
		// All parameters are validated and parsed!
		return &GetUserResponse{
			UserID: req.UserID,
			Name:   "John Doe",
			Email:  "john@example.com",
		}, nil
	})

	// PUT /users/:id - Update user with path, header, and body validation
	sprout.PUT(router, "/users/:id", func(ctx context.Context, req *UpdateUserRequest) (*UpdateUserResponse, error) {
		return &UpdateUserResponse{
			UserID:  req.UserID,
			Name:    req.Name,
			Email:   req.Email,
			Message: "User updated successfully",
		}, nil
	})

	// Start the server
	log.Fatal(http.ListenAndServe(":8080", router))
}

// Example with typed error handling
type NotFoundError struct {
	Resource string `json:"resource" validate:"required"`
	ID       string `json:"id" validate:"required"`
}

func (e NotFoundError) Error() string {
	return fmt.Sprintf("%s not found: %s", e.Resource, e.ID)
}

func (e NotFoundError) StatusCode() int {
	return http.StatusNotFound
}

func (e NotFoundError) ResponseBody() interface{} {
	return e
}

type ConflictError struct {
	Field   string `json:"field" validate:"required"`
	Message string `json:"message" validate:"required"`
}

func (e ConflictError) Error() string {
	return e.Message
}

func (e ConflictError) StatusCode() int {
	return http.StatusConflict
}

func (e ConflictError) ResponseBody() interface{} {
	return e
}

type UnauthorizedError struct {
	Message string `json:"message" validate:"required"`
}

func (e UnauthorizedError) Error() string {
	return e.Message
}

func (e UnauthorizedError) StatusCode() int {
	return http.StatusUnauthorized
}

func (e UnauthorizedError) ResponseBody() interface{} {
	return e
}

func Example_withErrorHandling() {
	router := sprout.New()

	// Register endpoint with expected error types
	// Error response bodies are automatically validated just like success responses
	sprout.POST(router, "/users", func(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
		// Check if user already exists - returns 409 Conflict
		if req.Email == "existing@example.com" {
			return nil, ConflictError{
				Field:   "email",
				Message: "email already exists",
			}
		}

		// Check authorization - returns 401 Unauthorized
		if req.Age < 18 {
			return nil, UnauthorizedError{
				Message: "must be 18 or older",
			}
		}

		return &CreateUserResponse{
			ID:      123,
			Name:    req.Name,
			Email:   req.Email,
			Message: "User created successfully",
		}, nil
	}, sprout.WithErrors(
		NotFoundError{},     // Documents that this endpoint may return 404
		ConflictError{},     // Documents that this endpoint may return 409
		UnauthorizedError{}, // Documents that this endpoint may return 401
	))

	// GET endpoint that may return NotFoundError - returns 404
	sprout.GET(router, "/users/:id", func(ctx context.Context, req *GetUserRequest) (*GetUserResponse, error) {
		// Simulate user not found
		if req.UserID == "00000000-0000-0000-0000-000000000000" {
			return nil, NotFoundError{
				Resource: "user",
				ID:       req.UserID,
			}
		}

		return &GetUserResponse{
			UserID: req.UserID,
			Name:   "John Doe",
			Email:  "john@example.com",
		}, nil
	}, sprout.WithErrors(NotFoundError{}, UnauthorizedError{}))

	log.Fatal(http.ListenAndServe(":8080", router))
}

// Example showing error validation in action
func Example_errorValidation() {
	router := sprout.New()

	// This demonstrates that error response bodies are validated
	// If you return an error with invalid/missing required fields,
	// Sprout will catch it and return 500 Internal Server Error
	sprout.GET(router, "/invalid", func(ctx context.Context, req *GetUserRequest) (*GetUserResponse, error) {
		// This error is INVALID - missing required ID field
		// Sprout will validate and return: "Error response validation failed"
		return nil, NotFoundError{
			Resource: "user",
			// ID is MISSING! This will fail validation since it's marked as required
		}
	}, sprout.WithErrors(NotFoundError{}))

	// This is the CORRECT way - all required fields provided
	sprout.GET(router, "/valid", func(ctx context.Context, req *GetUserRequest) (*GetUserResponse, error) {
		// This error is VALID - all required fields present
		return nil, NotFoundError{
			Resource: "user",
			ID:       req.UserID, // All validation tags satisfied
		}
	}, sprout.WithErrors(NotFoundError{}))

	log.Fatal(http.ListenAndServe(":8080", router))
}
