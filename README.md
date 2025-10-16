# sprout

A type-safe HTTP router for Go that provides automatic validation and parameter binding using struct tags. Built on top of [httprouter](https://github.com/julienschmidt/httprouter) for high performance.

## Features

- ✨ **Type-safe handlers** using Go generics
- 🔒 **Automatic request & response validation** via `go-playground/validator`
- ⚠️ **Typed error responses** with automatic validation and status codes
- 🎯 **Multi-source parameter binding** - path, query, headers, and body in one struct
- 📤 **Response headers** - set custom HTTP headers using struct tags
- 🧹 **Auto-exclusion** - routing/metadata fields automatically excluded from JSON
- 🔄 **Automatic type conversion** - strings to int, float, bool, etc.
- 📭 **Empty responses** - return `nil` for empty responses, validated against type contract
- 🚀 **High performance** - powered by httprouter
- 📝 **Self-documenting APIs** - request/response contracts visible in code

## Installation

```bash
go get github.com/mayask/sprout
```

## Quick Start

```go
package main

import (
    "context"
    "log"
    "net/http"

    "github.com/mayask/sprout"
)

type CreateUserRequest struct {
    Name  string `json:"name" validate:"required,min=3"`
    Email string `json:"email" validate:"required,email"`
}

type CreateUserResponse struct {
    ID    int    `json:"id" validate:"required"`
    Name  string `json:"name" validate:"required"`
    Email string `json:"email" validate:"required"`
}

func main() {
    router := sprout.New()

    sprout.POST(router, "/users", func(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
        // Request is already parsed and validated!
        return &CreateUserResponse{
            ID:    123,
            Name:  req.Name,
            Email: req.Email,
        }, nil
    })

    log.Fatal(http.ListenAndServe(":8080", router))
}
```

## Parameter Binding

Sprout can automatically extract and validate parameters from multiple sources using struct tags.

### Path Parameters

Extract dynamic segments from the URL path:

```go
type GetUserRequest struct {
    UserID string `path:"id" validate:"required,uuid4"`
}

// Route: /users/:id
sprout.GET(router, "/users/:id", func(ctx context.Context, req *GetUserRequest) (*UserResponse, error) {
    // req.UserID contains the :id path parameter
    return &UserResponse{ID: req.UserID}, nil
})
```

### Query Parameters

Extract and validate query string parameters with automatic type conversion:

```go
type SearchRequest struct {
    Query  string `query:"q" validate:"required,min=1"`
    Page   int    `query:"page" validate:"omitempty,gte=1"`
    Limit  int    `query:"limit" validate:"omitempty,gte=1,lte=100"`
    Active bool   `query:"active"`
}

// Route: /search?q=golang&page=2&limit=20&active=true
sprout.GET(router, "/search", func(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
    // All query params are parsed and validated
    return &SearchResponse{Results: []string{}}, nil
})
```

### Headers

Validate HTTP headers:

```go
type SecureRequest struct {
    AuthToken string `header:"Authorization" validate:"required"`
    UserAgent string `header:"User-Agent" validate:"required"`
}

sprout.GET(router, "/secure", func(ctx context.Context, req *SecureRequest) (*Response, error) {
    // Headers are validated
    return &Response{Status: "ok"}, nil
})
```

### Request Body

Parse and validate JSON request bodies:

```go
type UpdateProfileRequest struct {
    Name     string `json:"name" validate:"required,min=3,max=100"`
    Bio      string `json:"bio" validate:"omitempty,max=500"`
    Age      int    `json:"age" validate:"required,gte=18,lte=120"`
    Website  string `json:"website" validate:"omitempty,url"`
}

sprout.PUT(router, "/profile", func(ctx context.Context, req *UpdateProfileRequest) (*Response, error) {
    // JSON body is parsed and validated
    return &Response{Message: "Profile updated"}, nil
})
```

#### Nested Objects in Request Body

Sprout supports nested objects with full validation:

```go
type Address struct {
    Street  string `json:"street" validate:"required"`
    City    string `json:"city" validate:"required"`
    ZipCode string `json:"zip_code" validate:"required,len=5"`
    Country string `json:"country" validate:"required,len=2"` // ISO country code
}

type CreateUserRequest struct {
    Name    string  `json:"name" validate:"required,min=3"`
    Email   string  `json:"email" validate:"required,email"`
    Address Address `json:"address" validate:"required"`
}

// Example JSON payload:
// {
//   "name": "John Doe",
//   "email": "john@example.com",
//   "address": {
//     "street": "123 Main St",
//     "city": "New York",
//     "zip_code": "10001",
//     "country": "US"
//   }
// }

sprout.POST(router, "/users", func(ctx context.Context, req *CreateUserRequest) (*UserResponse, error) {
    // Nested objects are automatically parsed and validated
    return &UserResponse{ID: "123", Name: req.Name}, nil
})
```

### Combining Multiple Sources

You can combine path, query, headers, and body (including nested objects) in a single request struct:

```go
type Address struct {
    Street  string `json:"street" validate:"required"`
    City    string `json:"city" validate:"required"`
    ZipCode string `json:"zip_code" validate:"required"`
}

type UpdateUserRequest struct {
    // Path parameter
    UserID string `path:"id" validate:"required,uuid4"`

    // Header
    AuthToken string `header:"Authorization" validate:"required,startswith=Bearer "`

    // Query parameters
    Notify bool `query:"notify"`

    // JSON body fields (including nested objects)
    Name    string  `json:"name" validate:"required,min=3"`
    Email   string  `json:"email" validate:"required,email"`
    Age     int     `json:"age" validate:"required,gte=18"`
    Address Address `json:"address" validate:"required"`
}

type UpdateUserResponse struct {
    UserID  string  `json:"user_id" validate:"required"`
    Name    string  `json:"name" validate:"required"`
    Email   string  `json:"email" validate:"required"`
    Address Address `json:"address" validate:"required"`
    Updated bool    `json:"updated" validate:"required"`
}

sprout.PUT(router, "/users/:id", func(ctx context.Context, req *UpdateUserRequest) (*UpdateUserResponse, error) {
    // All parameters from different sources are available, including nested objects
    return &UpdateUserResponse{
        UserID:  req.UserID,
        Name:    req.Name,
        Email:   req.Email,
        Address: req.Address,
        Updated: true,
    }, nil
})
```

## Validation

Sprout validates both requests **and** responses using [go-playground/validator](https://github.com/go-playground/validator) tags.

### Common Validation Tags

```go
type ExampleRequest struct {
    // String validations
    Name     string `validate:"required"`              // Must be present
    Username string `validate:"required,min=3,max=20"` // Length constraints
    Email    string `validate:"required,email"`        // Email format
    URL      string `validate:"omitempty,url"`         // URL format (optional)

    // Numeric validations
    Age      int     `validate:"required,gte=18,lte=120"` // Range constraints
    Price    float64 `validate:"required,gt=0"`            // Greater than
    Quantity uint    `validate:"omitempty,lte=1000"`       // Less than or equal

    // Conditional validations
    Password string `validate:"required_with=NewPassword,min=8"` // Required if NewPassword present

    // Custom formats
    UUID     string `validate:"required,uuid4"`         // UUID v4 format
    Color    string `validate:"required,hexcolor"`      // Hex color
    IP       string `validate:"required,ip"`            // IP address
}
```

See the [validator documentation](https://pkg.go.dev/github.com/go-playground/validator/v10) for all available validation tags.

## Supported HTTP Methods

All standard HTTP methods are supported:

```go
sprout.GET(router, "/path", handler)
sprout.POST(router, "/path", handler)
sprout.PUT(router, "/path", handler)
sprout.PATCH(router, "/path", handler)
sprout.DELETE(router, "/path", handler)
sprout.HEAD(router, "/path", handler)
sprout.OPTIONS(router, "/path", handler)
```

## Base Path

You can define a base path that will be prepended to all routes registered with a router. This is useful for API versioning or organizing routes under a common prefix.

```go
config := &sprout.Config{
    BasePath: "/api/v1",
}
router := sprout.NewWithConfig(config)

// Register routes without the base path
sprout.GET(router, "/users", handleListUsers)      // Accessible at /api/v1/users
sprout.POST(router, "/users", handleCreateUser)    // Accessible at /api/v1/users
sprout.GET(router, "/users/:id", handleGetUser)    // Accessible at /api/v1/users/:id
sprout.DELETE(router, "/users/:id", handleDeleteUser) // Accessible at /api/v1/users/:id
```

## Type Conversion

Query parameters, path parameters, and headers are automatically converted from strings to the appropriate type:

| Go Type | Supported |
|---------|-----------|
| `string` | ✅ |
| `int`, `int8`, `int16`, `int32`, `int64` | ✅ |
| `uint`, `uint8`, `uint16`, `uint32`, `uint64` | ✅ |
| `float32`, `float64` | ✅ |
| `bool` | ✅ |

## Error Handling

### Basic Error Responses

Sprout automatically returns appropriate HTTP status codes:

| Status Code | When |
|-------------|------|
| `400 Bad Request` | Invalid JSON, parameter parsing errors, or validation failures |
| `500 Internal Server Error` | Handler errors or response validation failures |

Example error response for validation failure:
```
Request validation failed: Key: 'CreateUserRequest.Email' Error:Field validation for 'Email' failed on the 'email' tag
```

### Typed Error Responses

For more control over error responses, define error types with struct tags for status codes:

```go
// Define a typed error with status code in struct tag
type NotFoundError struct {
    _        struct{} `http:"status=404"`
    Resource string   `json:"resource" validate:"required"`
    ID       string   `json:"id" validate:"required"`
    Message  string   `json:"message" validate:"required"`
}

func (e NotFoundError) Error() string {
    return fmt.Sprintf("%s not found: %s", e.Resource, e.ID)
}

// Use in handlers
sprout.GET(router, "/users/:id", func(ctx context.Context, req *GetUserRequest) (*UserResponse, error) {
    user, err := db.FindUser(req.UserID)
    if err != nil {
        return nil, NotFoundError{
            Resource: "user",
            ID:       req.UserID,
            Message:  "user not found",
        }
    }
    return &UserResponse{ID: user.ID, Name: user.Name}, nil
}, sprout.WithErrors(NotFoundError{}))
```

**Key features:**
- Error response bodies are **automatically validated** using the same validation tags
- Status codes defined via struct tags: `http:"status=404"`
- The error struct itself is serialized as the response body
- Type-safe error responses with struct validation
- Optional error type registration via `WithErrors()` for compile-time documentation and OpenAPI generation

### Multiple Error Types

You can register multiple expected error types for documentation and validation:

```go
type ConflictError struct {
    _       struct{} `http:"status=409"`
    Field   string   `json:"field" validate:"required"`
    Message string   `json:"message" validate:"required"`
}

func (e ConflictError) Error() string { return e.Message }

type UnauthorizedError struct {
    _       struct{} `http:"status=401"`
    Message string   `json:"message" validate:"required"`
}

func (e UnauthorizedError) Error() string { return e.Message }

// Register all possible error types
sprout.POST(router, "/users", func(ctx context.Context, req *CreateUserRequest) (*UserResponse, error) {
    // Check authorization
    if !isAuthorized(ctx) {
        return nil, UnauthorizedError{Message: "invalid credentials"}
    }

    // Check for conflicts
    if userExists(req.Email) {
        return nil, ConflictError{Field: "email", Message: "email already exists"}
    }

    // Check if resource exists
    if !resourceExists(req.OrgID) {
        return nil, NotFoundError{Resource: "organization", ID: req.OrgID, Message: "organization not found"}
    }

    return &UserResponse{ID: "123", Name: req.Name}, nil
}, sprout.WithErrors(
    NotFoundError{},
    ConflictError{},
    UnauthorizedError{},
))
```

The `WithErrors()` option provides:
- **Runtime validation**: Enforces declared error types (configurable)
- **Self-documentation**: Makes possible error responses explicit in code
- **Type safety**: Error response bodies are validated before sending
- **OpenAPI generation**: Status codes and schemas accessible via reflection for documentation

### Strict Error Type Checking

By default, Sprout enforces that handlers only return error types explicitly declared via `WithErrors()`. This encourages well-documented APIs and prevents unexpected error responses.

#### Default Behavior (Strict Mode)

If a handler returns an undeclared error type, Sprout returns `500 Internal Server Error`:

```go
sprout.POST(router, "/users", func(ctx context.Context, req *CreateUserRequest) (*UserResponse, error) {
    if userExists(req.Email) {
        // ❌ ConflictError is declared, so this works
        return nil, ConflictError{Field: "email", Message: "email already exists"}
    }

    if !authorized {
        // ❌ ERROR! UnauthorizedError is NOT declared - returns 500
        return nil, UnauthorizedError{Message: "not authorized"}
    }

    return &UserResponse{ID: "123"}, nil
}, sprout.WithErrors(ConflictError{})) // Only ConflictError declared
```

**Log output:**
```
ERROR: handler returned undeclared error type: UnauthorizedError (expected one of: [ConflictError])
```

**Client receives:**
```
HTTP/1.1 500 Internal Server Error
undeclared_error_type: handler returned undeclared error type: UnauthorizedError
```

#### Disabling Strict Mode

To allow undeclared error types (backward compatibility mode), set `StrictErrorTypes` to `false`:

```go
falseVal := false
config := &sprout.Config{
    StrictErrorTypes: &falseVal,
}
router := sprout.NewWithConfig(config)

sprout.POST(router, "/users", func(ctx context.Context, req *CreateUserRequest) (*UserResponse, error) {
    // Now undeclared errors are allowed (with warning log)
    return nil, UnauthorizedError{Message: "not authorized"}
}, sprout.WithErrors(ConflictError{}))
```

**Log output:**
```
WARNING: handler returned unexpected error type: UnauthorizedError (expected one of: [ConflictError])
```

**Client receives:**
```
HTTP/1.1 401 Unauthorized
{"message": "not authorized"}
```

#### Handling Undeclared Errors with Custom Error Handler

When using a custom error handler, you can detect and handle undeclared error types:

```go
config := &sprout.Config{
    ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
        var sproutErr *sprout.Error
        if errors.As(err, &sproutErr) {
            // Check if this is an undeclared error type
            if sproutErr.Kind == sprout.ErrorKindUndeclaredError {
                // Log to monitoring system
                logToSentry(sproutErr)

                // Return custom response
                w.Header().Set("Content-Type", "application/json")
                w.WriteHeader(http.StatusInternalServerError)
                json.NewEncoder(w).Encode(map[string]string{
                    "error": "internal_error",
                    "message": "An unexpected error occurred",
                })
                return
            }
        }

        // Handle other error kinds...
    },
}
```

**Benefits of strict mode (default):**
- Forces explicit error type declarations via `WithErrors()`
- Makes API contracts clear and self-documenting
- Catches missing error type declarations during development
- Helps generate accurate OpenAPI/Swagger documentation

**When to disable strict mode:**
- Migrating legacy code that doesn't use `WithErrors()`
- Prototyping where error handling isn't finalized
- Using dynamic error types that can't be known at compile time

### Custom Error Handler

Sprout allows you to customize how system errors (parsing errors, validation errors, etc.) are handled and returned to clients. This gives you full control over error response formatting.

#### Using a Custom Error Handler

Create a router with a custom error handler using `NewWithConfig()`:

```go
config := &sprout.Config{
    ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
        // Extract sprout.Error for detailed error information
        var sproutErr *sprout.Error
        if errors.As(err, &sproutErr) {
            // Return custom JSON error response
            w.Header().Set("Content-Type", "application/json")

            status := http.StatusInternalServerError
            switch sproutErr.Kind {
            case sprout.ErrorKindParse, sprout.ErrorKindValidation:
                status = http.StatusBadRequest
            case sprout.ErrorKindResponseValidation, sprout.ErrorKindErrorValidation:
                status = http.StatusInternalServerError
            }

            w.WriteHeader(status)
            json.NewEncoder(w).Encode(map[string]any{
                "error": map[string]any{
                    "kind":    sproutErr.Kind,
                    "message": sproutErr.Message,
                    "details": sproutErr.Err.Error(),
                },
            })
            return
        }

        // Handle other errors
        http.Error(w, err.Error(), http.StatusInternalServerError)
    },
}

router := sprout.NewWithConfig(config)
```

#### Error Kinds

Sprout provides specific error kinds to help you handle different error scenarios:

| Error Kind | Description | Default Status |
|------------|-------------|----------------|
| `ErrorKindParse` | Failed to parse request parameters (query, path, headers) | 400 Bad Request |
| `ErrorKindValidation` | Request validation failed | 400 Bad Request |
| `ErrorKindNotFound` | No route matched the request (404) | 404 Not Found |
| `ErrorKindMethodNotAllowed` | HTTP method not allowed for route (405) | 405 Method Not Allowed |
| `ErrorKindResponseValidation` | Response validation failed (internal error) | 500 Internal Server Error |
| `ErrorKindErrorValidation` | Error response validation failed (internal error) | 500 Internal Server Error |
| `ErrorKindUndeclaredError` | Handler returned undeclared error type (when `StrictErrorTypes` is enabled) | 500 Internal Server Error |

#### Error Structure

The `sprout.Error` type provides detailed error context:

```go
type Error struct {
    Kind    ErrorKind  // Category of error
    Message string     // Human-readable message
    Err     error      // Underlying error (can be nil)
}
```

You can access the underlying error using `errors.As()` or `Unwrap()`:

```go
var sproutErr *sprout.Error
if errors.As(err, &sproutErr) {
    log.Printf("Error kind: %s", sproutErr.Kind)
    log.Printf("Message: %s", sproutErr.Message)
    if sproutErr.Err != nil {
        log.Printf("Underlying error: %v", sproutErr.Err)
    }
}
```

#### Default Error Handling

If no custom error handler is provided, Sprout uses sensible defaults:
- **Parse/Validation errors**: Returns `400 Bad Request` with plain text error message
- **404 Not Found**: Returns `404 Not Found` when no route matches
- **405 Method Not Allowed**: Returns `405 Method Not Allowed` when route exists but method doesn't match
- **Response/Error validation failures**: Returns `500 Internal Server Error` with plain text error message

```go
// Uses default error handling
router := sprout.New()
```

**Note**: 404 and 405 errors automatically go through your custom `ErrorHandler` (if configured), giving you consistent error formatting across all error types.

### Custom Success Status Codes

Response types can also define custom status codes using struct tags:

```go
type CreatedResponse struct {
    _       struct{} `http:"status=201"`  // 201 Created
    ID      int      `json:"id" validate:"required,gt=0"`
    Message string   `json:"message" validate:"required"`
}

sprout.POST(router, "/items", func(ctx context.Context, req *CreateItemRequest) (*CreatedResponse, error) {
    return &CreatedResponse{
        ID:      42,
        Message: "Item created successfully",
    }, nil
})
```

Without the `http` struct tag, responses default to `200 OK`.

### Custom Response Headers

You can set custom HTTP headers in both success and error responses using the `header:` tag:

```go
type UserCreatedResponse struct {
    _        struct{} `http:"status=201"`
    Location string   `header:"Location"`  // Set Location header
    ETag     string   `header:"ETag"`      // Set ETag header
    ID       string   `json:"id" validate:"required"`
    Name     string   `json:"name" validate:"required"`
}

sprout.POST(router, "/users", func(ctx context.Context, req *CreateUserRequest) (*UserCreatedResponse, error) {
    userID := "user-123"
    return &UserCreatedResponse{
        Location: fmt.Sprintf("/users/%s", userID),
        ETag:     `"v1.0"`,
        ID:       userID,
        Name:     req.Name,
    }, nil
})
```

The `Location` and `ETag` fields are automatically:
- Set as HTTP response headers
- **Excluded from the JSON response body** (no need for `json:"-"` tags!)

This works for error responses too:

```go
type RateLimitError struct {
    _            struct{} `http:"status=429"`
    RetryAfter   string   `header:"Retry-After"`    // Set Retry-After header
    RateLimit    string   `header:"X-Rate-Limit"`   // Set custom header
    Message      string   `json:"message" validate:"required"`
}

func (e RateLimitError) Error() string { return e.Message }
```

**Auto-exclusion from JSON**: Fields with `path`, `query`, `header`, or `http` tags are automatically excluded from JSON serialization. You don't need to add `json:"-"` manually!

### Empty Responses

For endpoints that don't need to return data (like DELETE operations), you can define empty response types and return `nil`:

```go
// Define an empty response type
type EmptyResponse struct{}

// Or with a custom status code
type NoContentResponse struct {
    _ struct{} `http:"status=204"`
}

// Handler can return nil
sprout.DELETE(router, "/users/:id", func(ctx context.Context, req *DeleteUserRequest) (*NoContentResponse, error) {
    // ... delete logic ...
    return nil, nil  // ✅ Returns 204 No Content with empty JSON body {}
})
```

**How it works:**

When a handler returns `nil` for the response, Sprout:
1. Creates an empty instance of the declared response type
2. Validates it against any validation tags
3. If validation passes (no required fields), serializes it as `{}`
4. If validation fails (has required fields), returns a validation error

## Access to httprouter Features

Since `Sprout` embeds `*httprouter.Router`, you have full access to all httprouter configuration and features:

```go
router := sprout.New()

// Configure httprouter settings
router.RedirectTrailingSlash = true
router.RedirectFixedPath = true
router.HandleMethodNotAllowed = true
router.HandleOPTIONS = true

// Set custom panic handler
router.PanicHandler = customPanicHandler

// Serve static files
router.ServeFiles("/static/*filepath", http.Dir("./public"))

// Use httprouter's native handlers for specific routes
router.Handle("GET", "/raw", func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
    w.Write([]byte("raw handler"))
})
```

## Complete Example

Here's a more complete example showing various features, including nested objects:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "net/http"

    "github.com/mayask/sprout"
)

// Nested types
type Address struct {
    Street  string `json:"street" validate:"required"`
    City    string `json:"city" validate:"required"`
    ZipCode string `json:"zip_code" validate:"required,len=5"`
    Country string `json:"country" validate:"required,len=2"`
}

type Preferences struct {
    Language     string `json:"language" validate:"required,oneof=en es fr de"`
    Timezone     string `json:"timezone" validate:"required"`
    Notifications bool   `json:"notifications"`
}

// List users with pagination
type ListUsersRequest struct {
    Page  int    `query:"page" validate:"omitempty,gte=1"`
    Limit int    `query:"limit" validate:"omitempty,gte=1,lte=100"`
    Token string `header:"Authorization" validate:"required"`
}

type ListUsersResponse struct {
    Users []User `json:"users" validate:"required"`
    Page  int    `json:"page" validate:"gte=1"`
    Total int    `json:"total" validate:"gte=0"`
}

// Get specific user
type GetUserRequest struct {
    UserID string `path:"id" validate:"required,uuid4"`
    Token  string `header:"Authorization" validate:"required"`
}

type UserResponse struct {
    ID          string      `json:"id" validate:"required"`
    Name        string      `json:"name" validate:"required"`
    Email       string      `json:"email" validate:"required,email"`
    Address     Address     `json:"address" validate:"required"`
    Preferences Preferences `json:"preferences" validate:"required"`
}

// Create user with nested objects
type CreateUserRequest struct {
    Name        string      `json:"name" validate:"required,min=3,max=100"`
    Email       string      `json:"email" validate:"required,email"`
    Age         int         `json:"age" validate:"required,gte=18,lte=120"`
    Address     Address     `json:"address" validate:"required"`
    Preferences Preferences `json:"preferences" validate:"required"`
}

// Update user
type UpdateUserRequest struct {
    UserID      string       `path:"id" validate:"required,uuid4"`
    Token       string       `header:"Authorization" validate:"required"`
    Name        string       `json:"name" validate:"omitempty,min=3,max=100"`
    Email       string       `json:"email" validate:"omitempty,email"`
    Address     *Address     `json:"address" validate:"omitempty"`     // Optional update
    Preferences *Preferences `json:"preferences" validate:"omitempty"` // Optional update
}

type User struct {
    ID          string      `json:"id"`
    Name        string      `json:"name"`
    Email       string      `json:"email"`
    Address     Address     `json:"address"`
    Preferences Preferences `json:"preferences"`
}

func main() {
    router := sprout.New()

    // List users with pagination
    sprout.GET(router, "/users", func(ctx context.Context, req *ListUsersRequest) (*ListUsersResponse, error) {
        page := req.Page
        if page == 0 {
            page = 1
        }
        limit := req.Limit
        if limit == 0 {
            limit = 10
        }

        return &ListUsersResponse{
            Users: []User{{
                ID:    "1",
                Name:  "John",
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
            }},
            Page:  page,
            Total: 1,
        }, nil
    })

    // Get user by ID with nested objects
    sprout.GET(router, "/users/:id", func(ctx context.Context, req *GetUserRequest) (*UserResponse, error) {
        return &UserResponse{
            ID:    req.UserID,
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
        }, nil
    })

    // Create new user with nested objects
    sprout.POST(router, "/users", func(ctx context.Context, req *CreateUserRequest) (*UserResponse, error) {
        return &UserResponse{
            ID:          "new-uuid",
            Name:        req.Name,
            Email:       req.Email,
            Address:     req.Address,     // Nested object from request
            Preferences: req.Preferences, // Nested object from request
        }, nil
    })

    // Update user (partial update with optional nested objects)
    sprout.PUT(router, "/users/:id", func(ctx context.Context, req *UpdateUserRequest) (*UserResponse, error) {
        // Start with existing user data
        response := &UserResponse{
            ID:    req.UserID,
            Name:  req.Name,
            Email: req.Email,
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
        }

        // Update nested objects if provided
        if req.Address != nil {
            response.Address = *req.Address
        }
        if req.Preferences != nil {
            response.Preferences = *req.Preferences
        }

        return response, nil
    })

    // Delete user
    sprout.DELETE(router, "/users/:id", func(ctx context.Context, req *GetUserRequest) (*UserResponse, error) {
        return &UserResponse{
            ID:    req.UserID,
            Name:  "Deleted User",
            Email: "deleted@example.com",
            Address: Address{
                Street:  "",
                City:    "",
                ZipCode: "",
                Country: "",
            },
            Preferences: Preferences{
                Language:      "en",
                Timezone:      "UTC",
                Notifications: false,
            },
        }, nil
    })

    fmt.Println("Server starting on :8080")
    log.Fatal(http.ListenAndServe(":8080", router))
}
```

## Testing

Sprout handlers are easy to test:

```go
func TestCreateUser(t *testing.T) {
    router := sprout.New()

    sprout.POST(router, "/users", func(ctx context.Context, req *CreateUserRequest) (*UserResponse, error) {
        return &UserResponse{
            ID:    "123",
            Name:  req.Name,
            Email: req.Email,
        }, nil
    })

    reqBody := CreateUserRequest{
        Name:  "John Doe",
        Email: "john@example.com",
        Age:   30,
    }
    body, _ := json.Marshal(reqBody)

    req := httptest.NewRequest("POST", "/users", bytes.NewReader(body))
    rec := httptest.NewRecorder()

    router.ServeHTTP(rec, req)

    assert.Equal(t, http.StatusOK, rec.Code)
}
```

## Requirements

- Go 1.18+ (for generics support)

## Dependencies

- [julienschmidt/httprouter](https://github.com/julienschmidt/httprouter) - High performance HTTP router
- [go-playground/validator](https://github.com/go-playground/validator) - Struct and field validation

## License

MIT

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
