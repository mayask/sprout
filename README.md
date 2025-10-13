# Sprout

A type-safe HTTP router for Go that provides automatic validation and parameter binding using struct tags. Built on top of [httprouter](https://github.com/julienschmidt/httprouter) for high performance.

## Features

✨ **Type-safe handlers** using Go generics
🔒 **Automatic request & response validation** via `go-playground/validator`
🎯 **Multi-source parameter binding** - path, query, headers, and body in one struct
🚀 **High performance** - powered by httprouter
📝 **Self-documenting APIs** - request/response contracts visible in code
🔄 **Automatic type conversion** - strings to int, float, bool, etc.

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

### Combining Multiple Sources

You can combine path, query, headers, and body in a single request struct:

```go
type UpdateUserRequest struct {
    // Path parameter
    UserID string `path:"id" validate:"required,uuid4"`

    // Header
    AuthToken string `header:"Authorization" validate:"required,startswith=Bearer "`

    // Query parameters
    Notify bool `query:"notify"`

    // JSON body fields
    Name  string `json:"name" validate:"required,min=3"`
    Email string `json:"email" validate:"required,email"`
    Age   int    `json:"age" validate:"required,gte=18"`
}

type UpdateUserResponse struct {
    UserID  string `json:"user_id" validate:"required"`
    Name    string `json:"name" validate:"required"`
    Email   string `json:"email" validate:"required"`
    Updated bool   `json:"updated" validate:"required"`
}

sprout.PUT(router, "/users/:id", func(ctx context.Context, req *UpdateUserRequest) (*UpdateUserResponse, error) {
    // All parameters from different sources are available
    return &UpdateUserResponse{
        UserID:  req.UserID,
        Name:    req.Name,
        Email:   req.Email,
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

## Type Conversion

Query parameters, path parameters, and headers are automatically converted from strings to the appropriate type:

| Go Type | Supported |
|---------|-----------|
| `string` | ✅ |
| `int`, `int8`, `int16`, `int32`, `int64` | ✅ |
| `uint`, `uint8`, `uint16`, `uint32`, `uint64` | ✅ |
| `float32`, `float64` | ✅ |
| `bool` | ✅ |

## Error Responses

Sprout automatically returns appropriate HTTP status codes:

| Status Code | When |
|-------------|------|
| `400 Bad Request` | Invalid JSON, parameter parsing errors, or validation failures |
| `500 Internal Server Error` | Handler errors or response validation failures |

Example error response for validation failure:
```
Request validation failed: Key: 'CreateUserRequest.Email' Error:Field validation for 'Email' failed on the 'email' tag
```

## Access to httprouter Features

Since `Sprout` embeds `*httprouter.Router`, you have full access to all httprouter configuration and features:

```go
router := sprout.New()

// Configure httprouter settings
router.RedirectTrailingSlash = true
router.RedirectFixedPath = true
router.HandleMethodNotAllowed = true
router.HandleOPTIONS = true

// Set custom handlers
router.NotFound = http.HandlerFunc(customNotFoundHandler)
router.MethodNotAllowed = http.HandlerFunc(customMethodNotAllowedHandler)
router.PanicHandler = customPanicHandler

// Serve static files
router.ServeFiles("/static/*filepath", http.Dir("./public"))

// Use httprouter's native handlers for specific routes
router.Handle("GET", "/raw", func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
    w.Write([]byte("raw handler"))
})
```

## Complete Example

Here's a more complete example showing various features:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "net/http"

    "github.com/mayask/sprout"
)

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
    ID    string `json:"id" validate:"required"`
    Name  string `json:"name" validate:"required"`
    Email string `json:"email" validate:"required,email"`
}

// Create user
type CreateUserRequest struct {
    Name  string `json:"name" validate:"required,min=3,max=100"`
    Email string `json:"email" validate:"required,email"`
    Age   int    `json:"age" validate:"required,gte=18,lte=120"`
}

// Update user
type UpdateUserRequest struct {
    UserID string `path:"id" validate:"required,uuid4"`
    Token  string `header:"Authorization" validate:"required"`
    Name   string `json:"name" validate:"omitempty,min=3,max=100"`
    Email  string `json:"email" validate:"omitempty,email"`
}

type User struct {
    ID    string `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email"`
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
            Users: []User{{ID: "1", Name: "John", Email: "john@example.com"}},
            Page:  page,
            Total: 1,
        }, nil
    })

    // Get user by ID
    sprout.GET(router, "/users/:id", func(ctx context.Context, req *GetUserRequest) (*UserResponse, error) {
        return &UserResponse{
            ID:    req.UserID,
            Name:  "John Doe",
            Email: "john@example.com",
        }, nil
    })

    // Create new user
    sprout.POST(router, "/users", func(ctx context.Context, req *CreateUserRequest) (*UserResponse, error) {
        return &UserResponse{
            ID:    "new-uuid",
            Name:  req.Name,
            Email: req.Email,
        }, nil
    })

    // Update user
    sprout.PUT(router, "/users/:id", func(ctx context.Context, req *UpdateUserRequest) (*UserResponse, error) {
        return &UserResponse{
            ID:    req.UserID,
            Name:  req.Name,
            Email: req.Email,
        }, nil
    })

    // Delete user
    sprout.DELETE(router, "/users/:id", func(ctx context.Context, req *GetUserRequest) (*UserResponse, error) {
        return &UserResponse{
            ID:    req.UserID,
            Name:  "Deleted User",
            Email: "deleted@example.com",
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
