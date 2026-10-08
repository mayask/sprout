package sprout

import (
	"reflect"

	"github.com/getkin/kin-openapi/openapi3"
)

// OpenAPISchemaResolver dynamically produces an OpenAPI schema for a Go type
// during route registration. It is called by the OpenAPI generator for every
// type encountered in request/response DTOs, after derefType normalization
// and before built-in/struct/scalar resolution.
//
// The resolver must be a pure function: given the same reflect.Type it should
// return the same inline schema (or nil); returning a $ref anywhere in the
// schema panics. It is invoked while the OpenAPI document mutex is held
// during route registration, so it MUST NOT call back into
// Sprout APIs (RegisterRoute, RegisterOpenAPISchemaResolver, etc.) or it will
// deadlock. Return nil for types the resolver does not handle; Sprout then
// falls back to its built-in schema generation.
//
// This hook runs only during OpenAPI document construction (route
// registration / swagger generation), never in the request/response hot path.
type OpenAPISchemaResolver func(t reflect.Type) *openapi3.SchemaRef

// WithOpenAPISchemaResolver sets a schema resolver that is consulted during
// route registration. Use this to provide OpenAPI schemas for custom value
// objects, string-backed enums, or domain types without importing Sprout from
// those packages. Pass this option before registering routes. On Mount it
// requires WithOpenAPIDocument and replaces the resolver copied from the parent.
func WithOpenAPISchemaResolver(r OpenAPISchemaResolver) Option {
	return func(cfg *Config) {
		cfg.openapiResolver = r
	}
}

// RegisterOpenAPISchemaResolver sets the schema resolver of the router's
// OpenAPI document after construction. Routes registered after this call use
// the resolver. Routes already registered are not retroactively rebuilt, so
// registration-before-routes is the documented happy path.
func (s *Sprout) RegisterOpenAPISchemaResolver(r OpenAPISchemaResolver) {
	if s == nil || s.openapi == nil {
		return
	}
	s.openapi.setResolver(r)
}
