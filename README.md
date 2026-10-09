# sprout

A type-safe HTTP router for Go that provides automatic validation and parameter binding using struct tags. Built on top of [httprouter](https://github.com/julienschmidt/httprouter) for high performance.

## Table of Contents

- [Features](#features)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Parameter Binding](#parameter-binding)
  - [Path Parameters](#path-parameters)
  - [Query Parameters](#query-parameters)
  - [Headers](#headers)
  - [Request Body](#request-body)
    - [Nested Objects in Request Body](#nested-objects-in-request-body)
    - [Streaming Request Bodies](#streaming-request-bodies)
  - [Combining Multiple Sources](#combining-multiple-sources)
- [Validation](#validation)
  - [Common Validation Tags](#common-validation-tags)
  - [Custom Validators](#custom-validators)
- [Supported HTTP Methods](#supported-http-methods)
- [Base Path](#base-path)
- [Nested Routers](#nested-routers)
- [Middleware](#middleware)
- [Type Conversion](#type-conversion)
- [Error Handling](#error-handling)
  - [Basic Error Responses](#basic-error-responses)
  - [Typed Error Responses](#typed-error-responses)
  - [Multiple Error Types](#multiple-error-types)
  - [Strict Error Type Checking](#strict-error-type-checking)
    - [Default Behavior (Strict Mode)](#default-behavior-strict-mode)
    - [Disabling Strict Mode](#disabling-strict-mode)
    - [Handling Undeclared Errors with Custom Error Handler](#handling-undeclared-errors-with-custom-error-handler)
  - [Custom Error Handler](#custom-error-handler)
    - [Using a Custom Error Handler](#using-a-custom-error-handler)
    - [Error Kinds](#error-kinds)
    - [Error Structure](#error-structure)
    - [Default Error Handling](#default-error-handling)
  - [Custom Success Status Codes](#custom-success-status-codes)
- [Custom Response Headers](#custom-response-headers)
- [Unwrapping Response Payloads](#unwrapping-response-payloads)
- [Empty Responses](#empty-responses)
- [OpenAPI & Swagger](#openapi--swagger)
  - [Customizing Metadata](#customizing-metadata)
  - [Schema Resolver](#schema-resolver)
  - [Component Names](#component-names)
  - [Separate Documents for Mounted Routers](#separate-documents-for-mounted-routers)
  - [Relative Paths](#relative-paths)
  - [Security](#security)
  - [Sample Server](#sample-server)
- [Access to httprouter Features](#access-to-httprouter-features)
- [Complete Example](#complete-example)
- [Testing](#testing)
- [Requirements](#requirements)
- [Dependencies](#dependencies)
- [License](#license)
- [Contributing](#contributing)

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

#### Array query parameters

Slice fields collect every occurrence of their key, in order. This is the OpenAPI default (`style: form`, `explode: true`):

```go
type ListPaymentsRequest struct {
    // /payments?wallet_ids=a&wallet_ids=b
    WalletIDs []string `query:"wallet_ids" validate:"omitempty,max=20,dive,uuid"`

    // /payments?currencies=GBP,EUR (repeated keys are accepted too)
    Currencies []string `query:"currencies" explode:"false"`
}
```

- Elements use the same conversion as scalar parameters, including named types and `encoding.TextUnmarshaler`.
- Add `explode:"false"` to also split each occurrence on commas. The generated OpenAPI parameter then declares `style: form, explode: false`. Without it, commas are part of the value.
- Empty items are skipped. A slice with no items stays `nil`; use `*[]T` to tell "absent" from "present".
- Validation runs on the whole slice (`max=20`) and, with `dive`, on each element (errors name `WalletIDs[0]`).
- Non-slice fields keep using the first occurrence of a repeated key.
- Bracket keys such as `wallet_ids[]=a` (axios' default array format) are not recognized. Configure the client to repeat plain keys, e.g. axios `paramsSerializer: { indexes: null }`.

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

#### Raw Request Bodies

Use `WithRawRequest()` for multipart uploads or other handlers that need to read the original body themselves. Sprout still parses and validates path, query, and header fields, but skips JSON body parsing.

```go
sprout.POST(router, "/uploads", func(ctx context.Context, req *UploadRequest) (*UploadResponse, error) {
    httpReq := sprout.HTTPRequest(ctx)
    reader, err := httpReq.MultipartReader()
    if err != nil {
        return nil, err
    }

    // Read multipart parts from reader.
    return &UploadResponse{Status: "ok"}, nil
}, sprout.WithRawRequest())
```

#### Streaming Request Bodies

Use an explicit `body` field with `*sprout.StreamBody` when the handler must
consume a large body incrementally without pre-reading, buffering, or temporary
files. The `contentType` tag drives both runtime validation and the generated
OpenAPI request body. Path, query, and header fields are populated and validated
before the handler receives the live stream.

```go
type CSVUploadRequest struct {
    Column *int `query:"column" validate:"required,gte=0"`

    Body *sprout.StreamBody `body:"" contentType:"text/csv" validate:"required"`
}

sprout.POST(router, "/uploads", func(ctx context.Context, req *CSVUploadRequest) (*UploadResponse, error) {
    // req.Body is the original live request stream. Process it incrementally.
    rows := csv.NewReader(req.Body)
    // ... read rows ...
    return &UploadResponse{Status: "ok"}, nil
}, sprout.WithRequestBodyLimit(1<<30))
```

Sprout closes the stream after the handler returns. `Close` is idempotent, so a
handler may close it explicitly when returning early. `WithRequestBodyLimit`
wraps the body in `http.MaxBytesReader`; reads beyond the limit return
`*http.MaxBytesError`.

Consuming body formats can be added through the decoder registry. Decoders
receive the request context, a body reader, parsed Content-Type parameters, and
the target value. They may return cleanup for multipart temporary files or
other resources. JSON and `application/x-www-form-urlencoded` are registered by
default; `StreamBody` bypasses this registry because it must remain unread until
the handler.

```go
router.RegisterRequestBodyDecoder("application/xml", func(
    ctx context.Context,
    body io.Reader,
    params map[string]string,
    target any,
) (cleanup func() error, err *sprout.Error) {
    // Decode body into target, then return optional cleanup.
})
```

The generated OpenAPI operation declares the configured media type with a
`string`/`binary` schema. An explicit `body` field cannot be combined with
`WithRawRequest()`.

#### Multiple Content Types and Form Bodies

A body field may accept several media types, separated by commas. The decoder is chosen by the request's `Content-Type`; any other type returns `400`, and the OpenAPI request body lists every accepted type with the same schema:

```go
type TokenRequest struct {
    Body TokenBody `body:"" contentType:"application/json,application/x-www-form-urlencoded"`
}

type TokenBody struct {
    GrantType    string `json:"grant_type" validate:"required,eq=client_credentials"`
    ClientID     string `json:"client_id" validate:"required"`
    ClientSecret string `json:"client_secret" validate:"required"`
}
```

The built-in `application/x-www-form-urlencoded` decoder:

- maps keys to fields by `json` tag name, overridable with a `form` tag (`form:"-"` skips a field);
- ignores unknown keys (e.g. OAuth clients sending `scope`);
- converts values like query parameters: scalars, named types, `encoding.TextUnmarshaler`, and slices from repeated keys; embedded structs are flattened;
- reports conversion failures as per-field validation errors, like JSON bodies.

Form data is flat, so registering a route whose form body has nested structs, maps, or slices of structs panics. Replacing the decoder with `RegisterRequestBodyDecoder` lifts this check. Response bodies (`*FileBody`) still declare exactly one content type.

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

> **Note:** Sprout initializes the validator with `validator.WithRequiredStructEnabled()`, opting into the stricter nesting rules that will become default in validator v11+.

### Defaults

A `default:"…"` tag supplies a value when a field is absent from the query string or from a JSON or form body. Defaults are applied before binding, so any value the client sends wins, and validation runs on the result:

```go
type ListRequest struct {
    Page int      `query:"page" default:"1" validate:"min=1"`
    Sort []string `query:"sort" default:"created_at,id"` // slices: comma-separated
}

type TokenBody struct {
    GrantType string `json:"grant_type" default:"client_credentials" validate:"eq=client_credentials"`
}
```

- Absent means: query key missing or empty; JSON key missing (or `null` on a non-pointer field); form key missing or empty. When the whole body is missing, a non-pointer body gets its defaults; a pointer body stays `nil`.
- Defaults convert like query values (scalars, named types, `encoding.TextUnmarshaler`, slices of those). An unconvertible default, a default on a path or header field, on the body field itself, or inside pointers, slices, or maps panics at registration. Nested defaults work through non-pointer structs.
- In OpenAPI the field gets `default` and is never listed as required.

OpenAPI also infers `enum` from `oneof=a b` and `eq=x` validation on scalar fields, and on slice items after `dive`. Tags combined with `|` are skipped, and fields whose schema is a component (for example a resolver enum) keep it.

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

### Custom Validators

You can extend the shared validator instance to add custom rules or custom type handling:

```go
import (
    "reflect"

    "github.com/go-playground/validator/v10"
)

router := sprout.New()

// Map custom types to validation-friendly values.
router.RegisterCustomTypeFunc(func(v reflect.Value) interface{} {
    if v.Kind() == reflect.Ptr && !v.IsNil() {
        v = v.Elem()
    }
    if wrapper, ok := v.Interface().(MyWrapper); ok {
        return wrapper.Value
    }
    return nil
}, MyWrapper{}, (*MyWrapper)(nil))

// Register a custom validation tag.
router.RegisterValidation("is-foo", func(fl validator.FieldLevel) bool {
    return fl.Field().String() == "foo"
})

type Payload struct {
    Value MyWrapper `validate:"is-foo"`
}
```

For validation that belongs to a Go type rather than a struct tag, register a type validator. Sprout recursively applies these callbacks to parsed request and response DTO values, which is useful for custom value objects such as string-backed enums:

```go
router.RegisterTypeValidationFunc(func(v reflect.Value) error {
    // Return nil for types this callback does not handle.
    return nil
})
```

`RegisterCustomTypeFunc` and `RegisterValidation` delegate to `go-playground/validator`. Type validators are Sprout-level callbacks that run in addition to tag-based validation. All customizations are available to routes mounted on the router and its children.

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

## Nested Routers

Create nested routers with shared error handling and path prefixes using `Mount`:

```go
router := sprout.New()

auth := router.Mount("/auth", nil)
sprout.POST(auth, "/login", handleAuthLogin)   // -> /auth/login
sprout.POST(auth, "/register", handleSignUp)   // -> /auth/register

api := router.Mount("/api", nil)
admin := api.Mount("/admin", nil)
sprout.GET(admin, "/users", handleAdminUsers)  // -> /api/admin/users
```

Child routers automatically reuse the parent's error handler and validator. Their base path is the combination of the parent's base path, the mount prefix, and any optional base path provided via the child configuration:

```go
apiV1 := router.Mount("/api", &sprout.Config{BasePath: "/v1"})
sprout.GET(apiV1, "/status", handleStatus) // -> /api/v1/status
```

Pass a full `sprout.Config` when mounting to override behavior per router (for example a distinct error handler or `StrictErrorTypes` flag) while leaving the parent untouched. Mounted routers share their parent's OpenAPI document unless they opt into their own; see [Separate Documents for Mounted Routers](#separate-documents-for-mounted-routers).

## Middleware

Attach middleware to any router with `Use()`. Middleware runs in the order it is registered and respects router hierarchy—parent middleware always wraps child middleware and routes, just like Express.

```go
router := sprout.New()

type AuthError struct {
	_       struct{} `http:"status=401"`
	Message string   `json:"message" validate:"required"`
}

// Global logging middleware
router.Use(func(w http.ResponseWriter, r *http.Request, next sprout.Next) {
	start := time.Now()
	next(nil) // continue to handlers
	log.Printf("%s %s (%s)", r.Method, r.URL.Path, time.Since(start))
})

api := router.Mount("/api", nil)

// Scoped middleware for /api/*
api.Use(func(w http.ResponseWriter, r *http.Request, next sprout.Next) {
	if r.Header.Get("Authorization") == "" {
		next(&AuthError{Message: "missing auth"})
		return
	}
	next(nil)
})

sprout.GET(api, "/users/:id", func(ctx context.Context, req *GetUserRequest) (*GetUserResponse, error) {
	// req already includes :id thanks to struct tags.
	// Middleware can still inspect the raw params via sprout.Params(r).
	return findUser(req.UserID), nil
})

// Route-level middleware using RouteOption
sprout.GET(api, "/reports", func(ctx context.Context, req *ReportRequest) (*ReportResponse, error) {
	return generateReport(req)
}, sprout.WithMiddleware(func(w http.ResponseWriter, r *http.Request, next sprout.Next) {
	if !hasReportAccess(r.Context()) {
		next(&AuthError{Message: "forbidden"})
		return
	}
	next(nil)
}))

// Bundle options that must always travel together into one RouteOption.
func requireScope(scope string) sprout.RouteOption {
	return sprout.RouteOptions(
		sprout.WithMiddleware(acl.Require(scope)),
		sprout.WithSecurity("oauth2", scope),
	)
}
```

### Falling Through with `ErrNext`

Typed handlers can opt to let downstream middleware handle a response by returning `sprout.ErrNext`. Middleware registered after the route will observe the fallthrough:

```go
sprout.GET(router, "/dashboard", func(ctx context.Context, req *EmptyRequest) (*DashboardResponse, error) {
	if isDeprecatedUser(ctx) {
		return nil, sprout.ErrNext // skip to the next middleware
	}
	return &DashboardResponse{Message: "Welcome back!"}, nil
})

router.Use(func(w http.ResponseWriter, r *http.Request, next sprout.Next) {
	// Runs when the handler called ErrNext or another middleware called next(nil)
	http.Redirect(w, r, "/upgrade", http.StatusFound)
})
```

### Accessing Route Parameters in Middleware

Middleware receives the raw `*http.Request`. Use `sprout.Params(r)` to read `httprouter.Params` captured for the route, even in fallback middleware for `404`/`405` responses:

```go
router.Use(func(w http.ResponseWriter, r *http.Request, next sprout.Next) {
	if params := sprout.Params(r); params != nil {
		log.Printf("matched route params: %#v", params)
	}
	next(nil)
})
```

> **Order matters:** Middleware registered before a route runs first. Middleware registered after a route only executes if the route (or earlier middleware) calls `next(nil)` or returns `sprout.ErrNext`. Middleware defined on parent routers wraps middleware/routes defined on child routers, so global behaviour is applied automatically. Use `next(err)` from any middleware to short-circuit the chain and run Sprout's error handling.

## OpenAPI & Swagger

Sprout now generates an OpenAPI 3.0 document using [kin-openapi](https://github.com/getkin/kin-openapi). Every registered route contributes path metadata, request/response schemas, and declared errors.

- The document is served at `/swagger` (or `<BasePath>/swagger` when a base path is configured).  
- JSON is returned by default; append `?format=yaml` for a YAML response.
- Programmatic access is available through `router.OpenAPIJSON()` and `router.OpenAPIYAML()`.

```go
router := sprout.New()
sprout.POST(router, "/users", handleCreateUser)

// Persist the generated spec
if data, err := router.OpenAPIJSON(); err == nil {
    _ = os.WriteFile("openapi.json", data, 0o644)
}
```

Schemas are derived from your request/response DTOs, path/query/header tags become parameters, and `WithErrors` contributes typed error responses—keeping the documentation aligned with the handlers.

### Customizing Metadata

Top-level OpenAPI metadata (title, version, contact details, etc.) is configured via router options:

```go
router := sprout.NewWithConfig(nil, sprout.WithOpenAPIInfo(sprout.OpenAPIInfo{
    Title:       "Payments API",
    Version:     "2025.04",
    Description: "Internal payments platform",
    Terms:       "https://example.com/terms",
    Contact: &sprout.OpenAPIContact{
        Name:  "API Support",
        Email: "support@example.com",
    },
    License: &sprout.OpenAPILicense{
        Name: "Apache-2.0",
        URL:  "https://www.apache.org/licenses/LICENSE-2.0",
    },
    Servers: []sprout.OpenAPIServer{
        {URL: "https://api.example.com", Description: "production"},
        {URL: "http://localhost:8080", Description: "local"},
    },
}))
```


### Schema Resolver

Sprout generates OpenAPI schemas automatically from your Go types, but some custom types need explicit schema metadata. Enum types backed by `string`, struct-backed value objects with unexported fields, or domain types from `internal/core` that must not import Sprout—these all need a way to tell the OpenAPI generator what shape they have on the wire.

`OpenAPISchemaResolver` is a pure function that produces a `*openapi3.SchemaRef` for any Go type Sprout encounters during route registration:

```go
type OpenAPISchemaResolver func(t reflect.Type) *openapi3.SchemaRef
```

Return `nil` for types the resolver does not handle; Sprout falls back to its built-in generation. The resolver runs only during OpenAPI document construction (route registration), never in the request/response hot path.

#### Registering a resolver

Pass a resolver at construction time:

```go
router := sprout.NewWithConfig(nil, sprout.WithOpenAPISchemaResolver(myResolver))
```

Or set one after construction (must be called before route registration):

```go
router := sprout.New()
router.RegisterOpenAPISchemaResolver(myResolver)
```

#### Resolution order

When Sprout encounters a type during route registration, it resolves schemas in this order:

1. **Resolver** — If registered, called first. If it returns a non-nil schema, that schema is used.
2. **Built-in `time.Time`** — Resolves to `{type: string, format: date-time}` (see below).
3. **Struct / slice / map / scalar** — Existing auto-generation from exported fields, element types, and Go kinds.

Both `schemaRefLocked` (struct, slice, map paths) and `inlineSchemaRefLocked` (inline scalar paths) consult the resolver, so named scalar types like `type UserStatus string` go through the resolver before falling back to plain string generation.

#### Component promotion and deduplication

For **named types** (`t.Name() != ""`), the resolver's output is promoted to a shared component in `doc.Components.Schemas`. Subsequent encounters of the same type (e.g. an enum type used in multiple response structs) get a `$ref` instead of inlining the schema. This keeps the generated OpenAPI document compact and ensures enum schemas are defined once.

#### Inline schemas only

Sprout owns `components/schemas`, so a resolver must return a fully inline schema: no `$ref` at the top level or in any nested `properties`, `items`, `additionalProperties`, `allOf`/`anyOf`/`oneOf`, or `not`. Route registration panics if a resolver returns a `$ref`, because nothing guarantees its target exists in the exported document.

#### Purity constraint

The resolver is called while the OpenAPI document mutex is held. It **must be a pure function**: given the same `reflect.Type`, it must return the same schema (or `nil`). It **must not** call back into Sprout APIs (`RegisterRoute`, `RegisterOpenAPISchemaResolver`, `OpenAPIJSON`, etc.) or it will deadlock.

#### Example: enum types

```go
func enumResolver(t reflect.Type) *openapi3.SchemaRef {
    // Check if the type has an Enum() []string method (string-backed enums).
    if t.Kind() == reflect.String {
        v := reflect.New(t).Elem().Interface()
        if e, ok := v.(interface{ Enum() []string }); ok {
            schema := openapi3.NewStringSchema()
            schema.Enum = make([]any, 0)
            for _, val := range e.Enum() {
                schema.Enum = append(schema.Enum, val)
            }
            return &openapi3.SchemaRef{Value: schema}
        }
    }
    return nil
}

router := sprout.NewWithConfig(nil,
    sprout.WithOpenAPISchemaResolver(enumResolver),
)
```

#### Built-in: `time.Time`

As of the same change that introduced the resolver, `time.Time` is handled as a built-in in `schemaRefLocked` and resolves to `{type: string, format: date-time}`. This replaces the previous behavior of emitting an empty object schema. Applications can override this by returning a non-nil schema for `time.Time` from their resolver—the resolver always takes priority.

The same metadata is available from the `/swagger` endpoint and through `OpenAPIJSON()` / `OpenAPIYAML()`.

### Component Names

Struct types and resolver-promoted named types become components named `<last package segment>_<TypeName>`, for example `payments_PaymentResponse`. When two different types in the same document would get the same name, every type in the clash moves one package segment deeper until the names differ, and existing `$ref`s are updated:

| Type | Component |
|---|---|
| `example.com/app/api/payments.PaymentResponse` | `api_payments_PaymentResponse` |
| `example.com/app/core/payments.PaymentResponse` | `core_payments_PaymentResponse` |
| `example.com/app/api/users.User` (no clash) | `users_User` |

Names depend only on the set of types in the document, not on registration order. Types that don't clash keep their short names. Adding a type that clashes with an existing one renames both. Registration panics only when two types cannot be told apart even by their full package path (for example, function-local types with the same name in one package).

### Separate Documents for Mounted Routers

A mounted router normally registers its routes into its parent's document. Pass `WithOpenAPIDocument` to give a mount, and every router mounted below it, an independent document with its own metadata, paths, and components:

```go
router := sprout.NewWithConfig(nil,
    sprout.WithOpenAPIInfo(sprout.OpenAPIInfo{Title: "Internal API", Version: "1.12.0"}),
    sprout.WithOpenAPISchemaResolver(enumResolver),
)
api := router.Mount("/api/v1", nil) // shares the root document

business := router.Mount("/business/v1", nil,
    sprout.WithOpenAPIDocument(sprout.OpenAPIInfo{Title: "Business API", Version: "1.0.0"}),
)
wallets := business.Mount("/wallets", nil) // uses the business document
sprout.GET(wallets, "/:id", getWallet)     // documented as /business/v1/wallets/{id}

internalSpec, _ := router.OpenAPIYAML()   // /api/v1/... only
businessSpec, _ := business.OpenAPIYAML() // /business/v1/... only
```

- Paths keep their full mounted prefix unless [`WithOpenAPIRelativePaths`](#relative-paths) is set.
- Each document contains only the components its own routes reference, so `$ref`s always resolve within that document and component names are deduplicated per document.
- The document starts with the parent's schema resolver as it is at `Mount` time. Pass `WithOpenAPISchemaResolver` alongside `WithOpenAPIDocument` to use a different one. Later `RegisterOpenAPISchemaResolver` calls affect only the document of the router they are called on.
- Routing is unaffected: the mount still shares the parent's HTTP router, middleware chain, error handling, and 404/405 behavior.
- The document is not served over HTTP automatically; only the root document is served at `/swagger`.
- OpenAPI document options (`WithOpenAPIInfo`, `WithOpenAPISchemaResolver`, `WithOpenAPIRelativePaths`, `WithOpenAPISecurityScheme`, `WithOpenAPISecurity`) on `Mount` without `WithOpenAPIDocument` panic, since a shared document belongs to its owner.

### Relative Paths

An API published on its own usually documents paths without the mount prefix. Add `WithOpenAPIRelativePaths()` next to `WithOpenAPIDocument` and declare servers that carry the prefix:

```go
business := router.Mount("/business/v1", nil,
    sprout.WithOpenAPIDocument(sprout.OpenAPIInfo{
        Title:   "Business API",
        Servers: []sprout.OpenAPIServer{{URL: "https://api.example.com/business/v1"}},
    }),
    sprout.WithOpenAPIRelativePaths(),
)
sprout.POST(business, "/auth/token", exchangeToken) // documented as /auth/token, operationId postAuthToken
```

Routing still uses the full path. Operation IDs are derived from the documented path, and two routes producing the same operation ID in one document (e.g. `/items/:id` and `/items/id`) panic at registration.

### Security

Declare security schemes with kin-openapi types, then document which routes need them. These options only describe security; enforce it in middleware.

```go
business := router.Mount("/business/v1", nil,
    sprout.WithOpenAPIDocument(info),
    sprout.WithOpenAPISecurityScheme("oauth2", &openapi3.SecurityScheme{
        Type: "oauth2",
        Flows: &openapi3.OAuthFlows{ClientCredentials: &openapi3.OAuthFlow{
            TokenURL: "https://api.example.com/business/v1/auth/token",
            Scopes:   map[string]string{"payments:view": "View payments"},
        }},
    }),
    sprout.WithOpenAPISecurity("oauth2"), // top-level security
)

sprout.POST(business, "/auth/token", exchangeToken) // before any default: security: []

business.Use(authMiddleware)
business.UseOpenAPISecurity("oauth2") // routes registered from here on

sprout.GET(business, "/me", getMe)                                         // inherits the top level
sprout.GET(business, "/payments", listPayments, sprout.WithSecurity("oauth2", "payments:view"))
sprout.GET(business, "/status", status, sprout.WithoutSecurity())         // security: []
```

```yaml
security:
  - oauth2: []
paths:
  /auth/token:
    post:
      security: []
  /me:
    get: {}            # no security field: the top level applies
  /payments:
    get:
      security:
        - oauth2: [payments:view]
```

- `WithOpenAPISecurityScheme(name, scheme)` adds `components.securitySchemes[name]`; extensions (`x-…`) on schemes and flows are preserved.
- `WithOpenAPISecurity(scheme, scopes...)` sets the document's top-level requirement. Operations whose security equals it omit their own field; every other operation is explicit, so routes without security get `security: []`. It does not secure routes by itself.
- `UseOpenAPISecurity(scheme, scopes...)` sets the default for routes registered afterwards on the router and its children, like `Use`. A later call replaces it. Defaults do not cross `WithOpenAPIDocument` boundaries.
- `WithSecurity(scheme, scopes...)` replaces the default for one route; repeat it to list alternatives. `WithoutSecurity()` marks a route public.
- Unknown scheme names, scopes not declared by the scheme's OAuth2 flows, and scopes on non-OAuth2/OpenID Connect schemes panic at registration.
- Relative `tokenUrl`s resolve against the server URL with standard URL rules (`/auth/token` drops the server path); absolute URLs avoid surprises.

### Sample Server

A runnable example lives in `cmd/demo/main.go`. Start it with:

```bash
go run ./cmd/demo
```

Browse endpoints like:

- `GET http://localhost:8080/ping`
- `POST http://localhost:8080/users`
- `GET http://localhost:8080/swagger` (append `?format=yaml` for YAML)

## Type Conversion

Query parameters, path parameters, and headers are automatically converted from strings to the appropriate type:

| Go Type | Supported |
|---------|-----------|
| `string` | ✅ |
| `int`, `int8`, `int16`, `int32`, `int64` | ✅ |
| `uint`, `uint8`, `uint16`, `uint32`, `uint64` | ✅ |
| `float32`, `float64` | ✅ |
| `bool` | ✅ |
| Types implementing `encoding.TextUnmarshaler` (e.g. `time.Time` as RFC 3339, `uuid.UUID`, `netip.Addr`) | ✅ |
| Slices of the above (query parameters only) | ✅ — see [Array query parameters](#array-query-parameters) |

Named types (`type Status string`) convert like their underlying kind. When a type implements `encoding.TextUnmarshaler`, `UnmarshalText` takes precedence over kind-based conversion, as in `encoding/json`, and its error becomes a parse error. Such types are documented as `type: string` in OpenAPI (`time.Time` as `format: date-time`) unless a schema resolver provides a schema.

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

#### Runtime Behavior Summary

| Scenario | `StrictErrorTypes = true` (default) | `StrictErrorTypes = false` |
| --- | --- | --- |
| Declared error passes validation | Serialized directly from the error struct, `ErrorHandler` not invoked | Same as strict |
| Declared error fails validation | Wrapped into `*sprout.Error` with `ErrorKindErrorValidation` and routed through `ErrorHandler` | Validation is skipped, the original error struct is serialized, `ErrorHandler` not invoked |
| Undeclared error returned | Wrapped into `*sprout.Error` with `ErrorKindUndeclaredError` and routed through `ErrorHandler` | Original error is passed to `ErrorHandler` unchanged (if configured); default handler still emits a 500 |

**Notes**

- Once a custom `ErrorHandler` is invoked, Sprout does not modify the HTTP response—your handler must write status, headers, and body.
- Typed error serialization happens before the `ErrorHandler` is called; only when serialization fails or strict-mode rules apply will Sprout call your handler.

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
            case sprout.ErrorKindNotFound:
                status = http.StatusNotFound
            case sprout.ErrorKindMethodNotAllowed:
                status = http.StatusMethodNotAllowed
            case sprout.ErrorKindResponseValidation, sprout.ErrorKindErrorValidation,
                 sprout.ErrorKindUndeclaredError, sprout.ErrorKindSerialization:
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
| `ErrorKindParse` | Malformed JSON or failed to parse request parameters (query, path, headers) | 400 Bad Request |
| `ErrorKindValidation` | Request validation or JSON body field decode failure | 400 Bad Request |
| `ErrorKindNotFound` | No route matched the request (404) | 404 Not Found |
| `ErrorKindMethodNotAllowed` | HTTP method not allowed for route (405) | 405 Method Not Allowed |
| `ErrorKindResponseValidation` | Response validation failed (internal error) | 500 Internal Server Error |
| `ErrorKindErrorValidation` | Error response validation failed (internal error) | 500 Internal Server Error |
| `ErrorKindUndeclaredError` | Handler returned undeclared error type (when `StrictErrorTypes` is enabled) | 500 Internal Server Error |
| `ErrorKindSerialization` | JSON encoding failed (internal error) | 500 Internal Server Error |

#### Error Structure

The `sprout.Error` type provides detailed error context:

```go
type Error struct {
    Kind    ErrorKind   // Category of error
    Message string      // Human-readable message
    Err     error       // Underlying error (can be nil)
    Fields  FieldErrors // Request field failures, for parse and validation errors
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

#### Field Errors

Parse and validation errors that concern request fields carry `Fields`, one entry per failure, named the way the client sent the value. Use it instead of inspecting `validator.ValidationErrors`, `TypeValidationErrors`, and `ParseParameterError` separately:

```go
type FieldError struct {
    Location ParameterSource // "body", "query", "path", or "header"
    Field    string          // request-relative path
    Tag      string          // validate tag (required, oneof, ...), "type", or "decode"
    Param    string          // tag parameter, e.g. "a b" for oneof=a b
    Kind     reflect.Kind    // kind of the field's Go type, when known
    Value    any             // offending value (raw input for "decode")
    Err      error           // cause for "type" and "decode"
}

if errors.As(err, &sproutErr) {
    for _, f := range sproutErr.Fields {
        log.Printf("%s %s: %s %s", f.Location, f.Field, f.Tag, f.Param)
    }
}
```

| Source | `Location` | `Field` example | `Tag` |
|---|---|---|---|
| Body field (explicit `body:""` or top-level JSON fields) | `body` | `grant_type`, `items[0].name` (JSON names; the `Body` field itself never appears) | validate tag, `type`, or `decode` |
| Form body | `body` | the form key (`json` name or `form` override) | validate tag or `decode` |
| Query / path / header | `query` / `path` / `header` | `wallet_ids[0]`, `id`, `X-Correlation-ID` | validate tag, `type`, or `decode` |

`decode` means the value could not be converted to the field's type (`ErrorKindParse` for parameters, `ErrorKindValidation` for body fields); `type` comes from `TypeValidationFunc`s. Malformed bodies that cannot be attributed to a field have no `Fields`. The validator is configured with the same names, so `validator.ValidationErrors` namespaces also use parameter names for query, path, and header fields.

#### Field-Aware JSON Body Decoding

For JSON object field decode failures, Sprout produces per-field errors with field paths instead of a body-level parse error:

- **Malformed JSON** (syntax errors, truncated body, top-level shape mismatch) → `ErrorKindParse` with the raw error
- **Object field decode errors** (custom `UnmarshalJSON` validation failures, field-level type mismatches) → `ErrorKindValidation` with `TypeValidationErrors` containing field paths

The happy path (`json.Unmarshal` succeeds) has zero overhead. The field-aware fallback runs only when the initial decode fails, walking the raw JSON and struct fields to produce granular errors:

```go
var sproutErr *sprout.Error
if errors.As(err, &sproutErr) {
    if sproutErr.Kind == sprout.ErrorKindValidation {
        var typeErrs sprout.TypeValidationErrors
        if errors.As(sproutErr.Err, &typeErrs) {
            for _, e := range typeErrs {
                log.Printf("Field: %s, Error: %v", e.Field, e.Err)
            }
        }
    }
}
```

Response validation uses the same struct tag validators and `TypeValidationFunc` callbacks as request validation, but does not involve a decode step — handlers construct responses in-process.

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

### Unwrapping Response Payloads

You can keep a struct response (for validation, headers, or status tags) and still emit a raw payload by marking exactly one field with `sprout:"unwrap"`:

```go
// UserResponse is the existing single-user DTO reused across the API.
type ListUsersResponse struct {
    Users []UserResponse `json:"users" sprout:"unwrap" validate:"required,dive"`
}

sprout.GET(router, "/users", func(ctx context.Context, req *ListUsersRequest) (*ListUsersResponse, error) {
    return &ListUsersResponse{
        Users: []UserResponse{
            {ID: "1", Name: "Alice", Email: "alice@example.com"},
            {ID: "2", Name: "Bob", Email: "bob@example.com"},
        },
    }, nil
})
```

The HTTP body produced by this handler is a bare JSON array (`[{ "id": "1", "name": "Alice", ... }, ...]`). The wrapper struct still participates in validation, can specify headers or status codes, and the generated OpenAPI schema reflects the unwrapped type.

Guidelines for `sprout:"unwrap"`:

- Only one exported field per response struct may declare `sprout:"unwrap"`.
- The tag is ignored on request DTOs; it's for responses only.
- Other fields in the struct continue to serialize normally (or are excluded if they carry routing/header tags).

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
