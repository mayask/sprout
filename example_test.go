package sprouter_test

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"sprouter"
)

// Define request DTO with validation tags
type CreateUserRequest struct {
	Name  string `json:"name" validate:"required,min=3,max=50"`
	Email string `json:"email" validate:"required,email"`
	Age   int    `json:"age" validate:"required,gte=18,lte=120"`
}

// Define response DTO with validation tags
type CreateUserResponse struct {
	ID      int    `json:"id" validate:"required,gt=0"`
	Name    string `json:"name" validate:"required"`
	Email   string `json:"email" validate:"required,email"`
	Message string `json:"message" validate:"required"`
}

func Example() {
	// Create a new router
	router := sprouter.New()

	// Register a handler with typed request/response DTOs
	// The framework will automatically:
	// - Parse JSON request body into CreateUserRequest
	// - Validate the request DTO
	// - Call your handler
	// - Validate the response DTO
	// - Serialize response as JSON
	sprouter.POST(router, "/users", func(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
		// Your business logic here
		// Request is already validated!
		return &CreateUserResponse{
			ID:      123,
			Name:    req.Name,
			Email:   req.Email,
			Message: fmt.Sprintf("User %s created successfully", req.Name),
		}, nil
	})

	// Start the server
	log.Fatal(http.ListenAndServe(":8080", router))
}
