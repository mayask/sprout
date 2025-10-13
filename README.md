# Sprouter

A type-safe HTTP router wrapper for Go that provides automatic validation of requests and responses using struct tags.

## Features

- **Type-safe handlers** with Go generics
- **Automatic JSON parsing** and serialization
- **Request validation** using `go-playground/validator` annotations
- **Response validation** to ensure API contract compliance
- **Multi-source parameter binding**: path params, query params, headers, and body
- Built on top of `julienschmidt/httprouter` for high performance

## Installation

```bash
go get sprouter
```

## Quick Start

### Simple POST with Body Validation

```go
type CreateUserRequest struct {
    Name  string `json:"name" validate:"required,min=3,max=50"`
    Email string `json:"email" validate:"required,email"`
    Age   int    `json:"age" validate:"required,gte=18,lte=120"`
}

type CreateUserResponse struct {
    ID      int    `json:"id" validate:"required,gt=0"`
    Name    string `json:"name" validate:"required"`
    Message string `json:"message" validate:"required"`
}

router := sprouter.New()
sprouter.POST(router, "/users", func(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
    // Request is already validated!
    return &CreateUserResponse{
        ID:      123,
        Name:    req.Name,
        Message: "User created successfully",
    }, nil
})
```

### GET with Path, Query, and Header Parameters

```go
type GetUserRequest struct {
    // Path parameter from URL /users/:id
    UserID string `path:"id" validate:"required"`

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

sprouter.GET(router, "/users/:id", func(ctx context.Context, req *GetUserRequest) (*GetUserResponse, error) {
    // All parameters are automatically parsed, validated, and type-converted!
    return &GetUserResponse{
        UserID: req.UserID,
        Name:   "John Doe",
        Email:  "john@example.com",
    }, nil
})
```

### PUT Combining Path, Headers, and Body

```go
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
    Message string `json:"message" validate:"required"`
}

sprouter.PUT(router, "/users/:id", func(ctx context.Context, req *UpdateUserRequest) (*UpdateUserResponse, error) {
    return &UpdateUserResponse{
        UserID:  req.UserID,
        Name:    req.Name,
        Message: "User updated successfully",
    }, nil
})
```

## Struct Tags

### Request Parameter Sources

- `path:"param_name"` - Extract from URL path parameters (e.g., `/users/:id`)
- `query:"param_name"` - Extract from URL query string (e.g., `?page=1`)
- `header:"Header-Name"` - Extract from HTTP headers
- `json:"field_name"` - Extract from JSON request body

### Validation Tags

Uses [go-playground/validator](https://github.com/go-playground/validator) syntax:

- `validate:"required"` - Field must be present
- `validate:"email"` - Must be valid email
- `validate:"min=3,max=50"` - String length constraints
- `validate:"gte=1,lte=100"` - Numeric range constraints
- `validate:"omitempty"` - Skip validation if empty

## Supported Types

The framework automatically converts string parameters to the following types:
- `string`
- `int`, `int8`, `int16`, `int32`, `int64`
- `uint`, `uint8`, `uint16`, `uint32`, `uint64`
- `float32`, `float64`
- `bool`

## HTTP Methods

Supports all standard HTTP methods:
- `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD`, `OPTIONS`

## Error Handling

The framework automatically returns appropriate HTTP status codes:
- `400 Bad Request` - Invalid JSON, parameter parsing errors, or validation failures
- `500 Internal Server Error` - Handler errors or response validation failures

## Access to httprouter Features

Since `Sprouter` embeds `*httprouter.Router`, you have full access to all httprouter features:

```go
router := sprouter.New()

// Configure httprouter settings
router.RedirectTrailingSlash = true
router.HandleMethodNotAllowed = true

// Use httprouter methods directly
router.ServeFiles("/static/*filepath", http.Dir("./public"))
router.NotFound = http.HandlerFunc(customNotFoundHandler)
```

## License

MIT
