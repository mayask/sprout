package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/mayask/sprout"
)

type pingResponse struct {
	Message string `json:"message" validate:"required"`
}

type createUserRequest struct {
	Name  string `json:"name" validate:"required"`
	Email string `json:"email" validate:"required,email"`
}

type createUserResponse struct {
	ID        int       `json:"id" validate:"required"`
	Name      string    `json:"name" validate:"required"`
	Email     string    `json:"email" validate:"required,email"`
	CreatedAt time.Time `json:"created_at" validate:"required"`
}

// conflictError provides a typed error example surfaced in OpenAPI.
type conflictError struct {
	_       struct{} `http:"status=409"`
	Message string   `json:"message" validate:"required"`
}

func (e *conflictError) Error() string {
	return e.Message
}

type getUserRequest struct {
	ID string `path:"id" validate:"required"`
}

type getUserResponse struct {
	ID    string `json:"id" validate:"required"`
	Name  string `json:"name" validate:"required"`
	Email string `json:"email" validate:"required,email"`
}

type pingRequest struct{}

func main() {
	router := sprout.NewWithConfig(nil, sprout.WithOpenAPIInfo(sprout.OpenAPIInfo{
		Title:       "Sprout Demo API",
		Version:     "2025.01",
		Description: "Sample API demonstrating Sprout routing and OpenAPI generation",
		Servers: []sprout.OpenAPIServer{
			{URL: "http://localhost:8080", Description: "local dev"},
		},
		Contact: &sprout.OpenAPIContact{
			Name:  "Sprout Maintainers",
			Email: "support@example.com",
		},
	}))

	sprout.GET(router, "/ping", func(ctx context.Context, _ *pingRequest) (*pingResponse, error) {
		return &pingResponse{Message: "pong"}, nil
	})

	sprout.POST(router, "/users", func(ctx context.Context, req *createUserRequest) (*createUserResponse, error) {
		if req.Email == "taken@example.com" {
			return nil, &conflictError{Message: "email already registered"}
		}

		return &createUserResponse{
			ID:        42,
			Name:      req.Name,
			Email:     req.Email,
			CreatedAt: time.Now(),
		}, nil
	}, sprout.WithErrors(&conflictError{}))

	sprout.GET(router, "/users/:id", func(ctx context.Context, req *getUserRequest) (*getUserResponse, error) {
		return &getUserResponse{
			ID:    req.ID,
			Name:  "Demo User",
			Email: "demo@example.com",
		}, nil
	})

	log.Println("listening on http://localhost:8080 (swagger at /swagger)")
	if err := http.ListenAndServe(":8080", router); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
