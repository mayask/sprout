package sprouter_test

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/mayask/sprouter"
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
	router := sprouter.New()

	// POST /users - Create user with body validation
	sprouter.POST(router, "/users", func(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
		return &CreateUserResponse{
			ID:      123,
			Name:    req.Name,
			Email:   req.Email,
			Message: fmt.Sprintf("User %s created successfully", req.Name),
		}, nil
	})

	// GET /users/:id - Get user with path, query, and header validation
	sprouter.GET(router, "/users/:id", func(ctx context.Context, req *GetUserRequest) (*GetUserResponse, error) {
		// All parameters are validated and parsed!
		return &GetUserResponse{
			UserID: req.UserID,
			Name:   "John Doe",
			Email:  "john@example.com",
		}, nil
	})

	// PUT /users/:id - Update user with path, header, and body validation
	sprouter.PUT(router, "/users/:id", func(ctx context.Context, req *UpdateUserRequest) (*UpdateUserResponse, error) {
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
