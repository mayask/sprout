package sprout_test

import (
	"context"
	"encoding/json"
	"errors"
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
	_        struct{} `http:"status=404"`
	Resource string   `json:"resource" validate:"required"`
	ID       string   `json:"id" validate:"required"`
}

func (e NotFoundError) Error() string {
	return fmt.Sprintf("%s not found: %s", e.Resource, e.ID)
}

type ConflictError struct {
	_       struct{} `http:"status=409"`
	Field   string   `json:"field" validate:"required"`
	Message string   `json:"message" validate:"required"`
}

func (e ConflictError) Error() string {
	return e.Message
}

type UnauthorizedError struct {
	_       struct{} `http:"status=401"`
	Message string   `json:"message" validate:"required"`
}

func (e UnauthorizedError) Error() string {
	return e.Message
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

// Example with custom response headers
// Response headers are set using the `header:` tag
// These fields are automatically excluded from JSON (no json:"-" needed!)

// Response type with custom headers
type UserCreatedResponse struct {
	_        struct{} `http:"status=201"`
	Location string   `header:"Location"` // Set Location header
	ETag     string   `header:"ETag"`     // Set ETag header
	ID       string   `json:"id" validate:"required"`
	Name     string   `json:"name" validate:"required"`
	Message  string   `json:"message" validate:"required"`
}

// Error type with custom headers
type RateLimitError struct {
	_          struct{} `http:"status=429"`
	RetryAfter string   `header:"Retry-After"` // Set Retry-After header
	RateLimit  string   `header:"X-RateLimit"` // Set custom header
	Message    string   `json:"message" validate:"required"`
}

func (e RateLimitError) Error() string { return e.Message }

func Example_withResponseHeaders() {
	router := sprout.New()

	// POST endpoint that returns 201 with Location and ETag headers
	sprout.POST(router, "/users", func(ctx context.Context, req *CreateUserRequest) (*UserCreatedResponse, error) {
		userID := "user-123"

		return &UserCreatedResponse{
			Location: fmt.Sprintf("/users/%s", userID), // Sets Location header
			ETag:     `"v1.0"`,                         // Sets ETag header
			ID:       userID,
			Name:     req.Name,
			Message:  "User created successfully",
		}, nil
		// JSON response: {"id":"user-123","name":"...","message":"..."}
		// HTTP headers: Location: /users/user-123, ETag: "v1.0"
		// Note: Location and ETag are NOT in the JSON body!
	})

	// Endpoint that may return rate limit error with custom headers
	sprout.GET(router, "/api/data", func(ctx context.Context, req *GetUserRequest) (*GetUserResponse, error) {
		// Simulate rate limiting
		if req.Page > 100 {
			return nil, RateLimitError{
				RetryAfter: "60",           // Sets Retry-After: 60 header
				RateLimit:  "100 per hour", // Sets X-RateLimit: 100 per hour header
				Message:    "rate limit exceeded",
			}
		}

		return &GetUserResponse{
			UserID: req.UserID,
			Name:   "John Doe",
			Email:  "john@example.com",
		}, nil
	}, sprout.WithErrors(RateLimitError{}))

	log.Fatal(http.ListenAndServe(":8080", router))
}

// Example with nested objects
// Nested objects are fully supported in requests and responses

// Nested type definitions
type Address struct {
	Street  string `json:"street" validate:"required"`
	City    string `json:"city" validate:"required"`
	ZipCode string `json:"zip_code" validate:"required,len=5"`
	Country string `json:"country" validate:"required,len=2"` // ISO country code
}

type Preferences struct {
	Language      string `json:"language" validate:"required,oneof=en es fr de"`
	Timezone      string `json:"timezone" validate:"required"`
	Notifications bool   `json:"notifications"`
}

type CreateUserWithNestedRequest struct {
	Name        string      `json:"name" validate:"required,min=3,max=50"`
	Email       string      `json:"email" validate:"required,email"`
	Age         int         `json:"age" validate:"required,gte=18,lte=120"`
	Address     Address     `json:"address" validate:"required"`
	Preferences Preferences `json:"preferences" validate:"required"`
}

type UserWithNestedResponse struct {
	ID          int         `json:"id" validate:"required,gt=0"`
	Name        string      `json:"name" validate:"required"`
	Email       string      `json:"email" validate:"required,email"`
	Address     Address     `json:"address" validate:"required"`
	Preferences Preferences `json:"preferences" validate:"required"`
	Message     string      `json:"message" validate:"required"`
}

func Example_withNestedObjects() {
	router := sprout.New()

	// POST /users - Create user with nested objects
	// Example request:
	// {
	//   "name": "John Doe",
	//   "email": "john@example.com",
	//   "age": 30,
	//   "address": {
	//     "street": "123 Main St",
	//     "city": "New York",
	//     "zip_code": "10001",
	//     "country": "US"
	//   },
	//   "preferences": {
	//     "language": "en",
	//     "timezone": "America/New_York",
	//     "notifications": true
	//   }
	// }
	sprout.POST(router, "/users", func(ctx context.Context, req *CreateUserWithNestedRequest) (*UserWithNestedResponse, error) {
		// All nested objects are parsed and validated automatically
		return &UserWithNestedResponse{
			ID:          123,
			Name:        req.Name,
			Email:       req.Email,
			Address:     req.Address,     // Nested object from request
			Preferences: req.Preferences, // Nested object from request
			Message:     fmt.Sprintf("User %s created at %s, %s", req.Name, req.Address.City, req.Address.Country),
		}, nil
	})

	// GET /users/:id - Get user with nested objects in response
	sprout.GET(router, "/users/:id", func(ctx context.Context, req *GetUserRequest) (*UserWithNestedResponse, error) {
		return &UserWithNestedResponse{
			ID:    123,
			Name:  "John Doe",
			Email: "john@example.com",
			Address: Address{
				Street:  "123 Main St",
				City:    "New York",
				ZipCode: "10001",
				Country: "US",
			},
			Preferences: Preferences{
				Language:      "en",
				Timezone:      "America/New_York",
				Notifications: true,
			},
			Message: "User retrieved successfully",
		}, nil
		// Response automatically serializes nested objects:
		// {
		//   "id": 123,
		//   "name": "John Doe",
		//   "email": "john@example.com",
		//   "address": {
		//     "street": "123 Main St",
		//     "city": "New York",
		//     "zip_code": "10001",
		//     "country": "US"
		//   },
		//   "preferences": {
		//     "language": "en",
		//     "timezone": "America/New_York",
		//     "notifications": true
		//   },
		//   "message": "User retrieved successfully"
		// }
	})

	log.Fatal(http.ListenAndServe(":8080", router))
}

// Example with custom error handler
// This shows how to customize system error responses (parsing, validation, etc.)
func Example_withCustomErrorHandler() {
	// Define custom error handler
	config := &sprout.Config{
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			// Extract sprout.Error for detailed error information
			var sproutErr *sprout.Error
			if errors.As(err, &sproutErr) {
				// Return structured JSON error response
				w.Header().Set("Content-Type", "application/json")

				// Determine status code based on error kind
				status := http.StatusInternalServerError
				switch sproutErr.Kind {
				case sprout.ErrorKindParse, sprout.ErrorKindValidation:
					status = http.StatusBadRequest
				case sprout.ErrorKindResponseValidation, sprout.ErrorKindErrorValidation:
					status = http.StatusInternalServerError
				}

				w.WriteHeader(status)

				// Return custom error format
				response := map[string]interface{}{
					"success": false,
					"error": map[string]interface{}{
						"type":    string(sproutErr.Kind),
						"message": sproutErr.Message,
					},
					"timestamp": "2024-01-01T00:00:00Z",
					"path":      r.URL.Path,
				}

				// Include underlying error details if available
				if sproutErr.Err != nil {
					response["error"].(map[string]interface{})["details"] = sproutErr.Err.Error()
				}

				json.NewEncoder(w).Encode(response)
				return
			}

			// Handle other errors (shouldn't normally happen with sprout)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		},
	}

	// Create router with custom error handler
	router := sprout.NewWithConfig(config)

	// Example endpoint that may trigger various error kinds
	sprout.POST(router, "/users", func(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
		return &CreateUserResponse{
			ID:      123,
			Name:    req.Name,
			Email:   req.Email,
			Message: "User created successfully",
		}, nil
	})

	// Example: Validation error (name too short)
	// POST /users with body: {"name": "Jo", "email": "john@example.com", "age": 25}
	// Response: 400 Bad Request
	// {
	//   "success": false,
	//   "error": {
	//     "type": "validation_error",
	//     "message": "request validation failed",
	//     "details": "Key: 'CreateUserRequest.Name' Error:Field validation for 'Name' failed on the 'min' tag"
	//   },
	//   "timestamp": "2024-01-01T00:00:00Z",
	//   "path": "/users"
	// }

	// Example endpoint with query params
	sprout.GET(router, "/search", func(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
		return &SearchResponse{
			Results: []string{"result1", "result2"},
			Total:   2,
		}, nil
	})

	// Example: Parse error (invalid page number)
	// GET /search?page=invalid&limit=10
	// Response: 400 Bad Request
	// {
	//   "success": false,
	//   "error": {
	//     "type": "parse_error",
	//     "message": "invalid query parameter 'page'",
	//     "details": "strconv.ParseInt: parsing \"invalid\": invalid syntax"
	//   },
	//   "timestamp": "2024-01-01T00:00:00Z",
	//   "path": "/search"
	// }

	log.Fatal(http.ListenAndServe(":8080", router))
}

// Example with strict error type checking
// By default, Sprout enforces that handlers only return declared error types
func Example_withStrictErrorTypes() {
	// Default behavior: strict mode enabled
	router := sprout.New()

	// Handler that declares only ConflictError
	sprout.POST(router, "/users", func(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
		// Check for conflicts
		if req.Email == "existing@example.com" {
			// ✅ OK - ConflictError is declared
			return nil, ConflictError{
				Field:   "email",
				Message: "email already exists",
			}
		}

		// Check authorization
		if req.Age < 18 {
			// ❌ ERROR! UnauthorizedError is NOT declared
			// In strict mode, this returns 500 Internal Server Error
			// Log: ERROR: handler returned undeclared error type: UnauthorizedError
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
	}, sprout.WithErrors(ConflictError{})) // Only ConflictError declared

	// To fix the above handler, declare ALL possible error types:
	sprout.POST(router, "/users-fixed", func(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
		if req.Email == "existing@example.com" {
			return nil, ConflictError{Field: "email", Message: "email already exists"}
		}
		if req.Age < 18 {
			// ✅ Now OK - UnauthorizedError is declared
			return nil, UnauthorizedError{Message: "must be 18 or older"}
		}
		return &CreateUserResponse{
			ID:      123,
			Name:    req.Name,
			Email:   req.Email,
			Message: "User created successfully",
		}, nil
	}, sprout.WithErrors(
		ConflictError{},     // Declare all possible error types
		UnauthorizedError{}, // This makes the API contract explicit
	))

	log.Fatal(http.ListenAndServe(":8080", router))
}

// Example disabling strict error type checking
// Sometimes you want to allow undeclared error types (backward compatibility, prototyping, etc.)
func Example_withoutStrictErrorTypes() {
	// Disable strict mode
	falseVal := false
	config := &sprout.Config{
		StrictErrorTypes: &falseVal, // Allow undeclared error types
	}
	router := sprout.NewWithConfig(config)

	// Now undeclared errors are allowed (with warning log)
	sprout.POST(router, "/users", func(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
		if req.Email == "existing@example.com" {
			return nil, ConflictError{Field: "email", Message: "email already exists"}
		}

		// ⚠️ UnauthorizedError not declared but still works (logs warning)
		// Log: WARNING: handler returned unexpected error type: UnauthorizedError
		// Client receives proper 401 response
		if req.Age < 18 {
			return nil, UnauthorizedError{Message: "must be 18 or older"}
		}

		return &CreateUserResponse{
			ID:      123,
			Name:    req.Name,
			Email:   req.Email,
			Message: "User created successfully",
		}, nil
	}, sprout.WithErrors(ConflictError{}))

	log.Fatal(http.ListenAndServe(":8080", router))
}

// Example handling undeclared errors in custom error handler
func Example_handlingUndeclaredErrorsInCustomHandler() {
	config := &sprout.Config{
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			var sproutErr *sprout.Error
			if errors.As(err, &sproutErr) {
				// Detect undeclared error types
				if sproutErr.Kind == sprout.ErrorKindUndeclaredError {
					// Log to monitoring/alerting system
					log.Printf("ALERT: Undeclared error type in handler: %v", sproutErr.Err)

					// Return custom response
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					json.NewEncoder(w).Encode(map[string]string{
						"error":   "internal_error",
						"message": "An unexpected error occurred. This has been logged.",
					})
					return
				}

				// Handle other error kinds normally
				status := http.StatusInternalServerError
				switch sproutErr.Kind {
				case sprout.ErrorKindParse, sprout.ErrorKindValidation:
					status = http.StatusBadRequest
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error": map[string]interface{}{
						"kind":    sproutErr.Kind,
						"message": sproutErr.Message,
					},
				})
				return
			}

			http.Error(w, err.Error(), http.StatusInternalServerError)
		},
	}

	router := sprout.NewWithConfig(config)

	// Handler with undeclared error will be caught by custom error handler
	sprout.POST(router, "/users", func(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
		if req.Age < 18 {
			// Undeclared error - custom handler will detect and handle it
			return nil, UnauthorizedError{Message: "must be 18 or older"}
		}
		return &CreateUserResponse{
			ID:      123,
			Name:    req.Name,
			Email:   req.Email,
			Message: "User created successfully",
		}, nil
	}, sprout.WithErrors(ConflictError{}))

	log.Fatal(http.ListenAndServe(":8080", router))
}

// Additional types for custom error handler example
type SearchRequest struct {
	Query string `query:"q" validate:"required"`
	Page  int    `query:"page" validate:"omitempty,gte=1"`
	Limit int    `query:"limit" validate:"omitempty,gte=1,lte=100"`
}

type SearchResponse struct {
	Results []string `json:"results" validate:"required"`
	Total   int      `json:"total" validate:"gte=0"`
}
