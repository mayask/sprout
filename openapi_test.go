package sprout

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

type conflictError struct {
	_       struct{} `http:"status=409"`
	Message string   `json:"message" validate:"required"`
}

type openAPIUser struct {
	ID   int    `json:"id" validate:"required"`
	Name string `json:"name" validate:"required"`
}

type openAPIEnvelope struct {
	Users []openAPIUser `json:"users" sprout:"unwrap" validate:"required,dive"`
}

func (e *conflictError) Error() string {
	return e.Message
}

func TestSwaggerEndpointReturnsOpenAPIJSON(t *testing.T) {
	router := New()

	type SwaggerRequest struct {
		ID     string `path:"id" validate:"required"`
		Search string `query:"search"`
	}

	type SwaggerResponse struct {
		Name string `json:"name" validate:"required"`
	}

	GET(router, "/users/:id", func(ctx context.Context, req *SwaggerRequest) (*SwaggerResponse, error) {
		return &SwaggerResponse{Name: "demo"}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/swagger", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected swagger endpoint to return 200, got %d", recorder.Code)
	}

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(recorder.Body.Bytes())
	if err != nil {
		t.Fatalf("failed to parse openapi document: %v", err)
	}

	pathItem := doc.Paths.Value("/users/{id}")
	if pathItem == nil {
		t.Fatalf("expected /users/{id} path in spec, got paths %v", pathKeys(doc.Paths))
	}

	op := pathItem.Get
	if op == nil {
		t.Fatalf("expected GET operation for /users/{id}")
	}

	if op.RequestBody != nil {
		t.Fatalf("did not expect request body for GET operation")
	}

	var sawPathID, sawQuery bool
	for _, p := range op.Parameters {
		if p == nil || p.Value == nil {
			continue
		}
		switch p.Value.In {
		case "path":
			if p.Value.Name == "id" && p.Value.Required {
				sawPathID = true
			}
		case "query":
			if p.Value.Name == "search" && !p.Value.Required {
				sawQuery = true
			}
		}
	}

	if !sawPathID {
		t.Fatalf("expected path parameter {id}")
	}
	if !sawQuery {
		t.Fatalf("expected optional query parameter 'search'")
	}

	resp := op.Responses.Value("200")
	if resp == nil || resp.Value == nil {
		t.Fatalf("expected 200 response in spec")
	}

	media := resp.Value.Content["application/json"]
	if media == nil || media.Schema == nil {
		t.Fatalf("expected application/json schema")
	}

	if media.Schema.Ref != "#/components/schemas/sprout_SwaggerResponse" {
		t.Fatalf("expected schema ref to sprout_SwaggerResponse, got %s", media.Schema.Ref)
	}

	if op.Responses.Value("default") == nil {
		t.Fatalf("expected default response to be registered")
	}

	yamlDoc, err := router.OpenAPIYAML()
	if err != nil {
		t.Fatalf("failed to generate openapi yaml: %v", err)
	}

	if !strings.Contains(string(yamlDoc), "/users/{id}") {
		t.Fatalf("expected yaml output to include path /users/{id}")
	}
}

func TestOpenAPIRequestBodyAndErrors(t *testing.T) {
	router := New()

	type CreateUserDTO struct {
		Name  string `json:"name" validate:"required"`
		Email string `json:"email" validate:"required,email"`
	}

	type CreateUserResponse struct {
		ID int `json:"id" validate:"required"`
	}

	POST(router, "/users", func(ctx context.Context, req *CreateUserDTO) (*CreateUserResponse, error) {
		return nil, &conflictError{Message: "exists"}
	}, WithErrors(&conflictError{}))

	specBytes, err := router.OpenAPIJSON()
	if err != nil {
		t.Fatalf("failed to marshal openapi json: %v", err)
	}

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(specBytes)
	if err != nil {
		t.Fatalf("failed to parse openapi json: %v", err)
	}

	pathItem := doc.Paths.Value("/users")
	if pathItem == nil {
		t.Fatalf("expected /users path in document")
	}

	op := pathItem.Post
	if op == nil {
		t.Fatalf("expected POST operation for /users")
	}

	if op.RequestBody == nil || op.RequestBody.Value == nil {
		t.Fatalf("expected request body to be documented")
	}

	if !op.RequestBody.Value.Required {
		t.Fatalf("expected request body to be required")
	}

	media := op.RequestBody.Value.Content["application/json"]
	if media == nil || media.Schema == nil {
		t.Fatalf("expected request body schema")
	}

	if media.Schema.Ref != "#/components/schemas/sprout_CreateUserDTO" {
		t.Fatalf("expected request schema ref, got %s", media.Schema.Ref)
	}

	resp := op.Responses.Value("409")
	if resp == nil || resp.Value == nil {
		t.Fatalf("expected 409 response for conflict error")
	}

	if _, ok := doc.Components.Schemas["sprout_conflictError"]; !ok {
		t.Fatalf("expected conflict error schema registered in components")
	}
}

func TestOpenAPIUnwrappedResponse(t *testing.T) {
	router := New()

	GET(router, "/users", func(ctx context.Context, req *EmptyRequest) (*openAPIEnvelope, error) {
		return &openAPIEnvelope{
			Users: []openAPIUser{
				{ID: 1, Name: "Alice"},
				{ID: 2, Name: "Bob"},
			},
		}, nil
	})

	specBytes, err := router.OpenAPIJSON()
	if err != nil {
		t.Fatalf("failed to marshal openapi json: %v", err)
	}

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(specBytes)
	if err != nil {
		t.Fatalf("failed to parse openapi json: %v", err)
	}

	pathItem := doc.Paths.Value("/users")
	if pathItem == nil {
		t.Fatalf("expected /users path in document")
	}

	op := pathItem.Get
	if op == nil {
		t.Fatalf("expected GET operation for /users")
	}

	resp := op.Responses.Value("200")
	if resp == nil || resp.Value == nil {
		t.Fatalf("expected 200 response in spec")
	}

	media := resp.Value.Content["application/json"]
	if media == nil || media.Schema == nil {
		t.Fatalf("expected application/json schema")
	}

	if media.Schema.Value == nil || !media.Schema.Value.Type.Is("array") {
		t.Fatalf("expected unwrapped response schema to be array, got %+v", media.Schema.Value)
	}

	if media.Schema.Value.Items == nil {
		t.Fatalf("expected array items schema")
	}

	if media.Schema.Value.Items.Ref != "#/components/schemas/sprout_openAPIUser" {
		t.Fatalf("expected items schema to reference sprout_openAPIUser, got %s", media.Schema.Value.Items.Ref)
	}

	if _, exists := doc.Components.Schemas["sprout_openAPIEnvelope"]; exists {
		t.Fatalf("did not expect envelope schema to be registered")
	}
}

func TestOpenAPIInfoOption(t *testing.T) {
	info := OpenAPIInfo{
		Title:       "Payments API",
		Version:     "2025.04",
		Description: "Internal payments gateway",
		Terms:       "https://example.com/terms",
		Contact: &OpenAPIContact{
			Name:  "API Support",
			Email: "support@example.com",
			URL:   "https://example.com/support",
		},
		License: &OpenAPILicense{
			Name: "Apache-2.0",
			URL:  "https://www.apache.org/licenses/LICENSE-2.0",
		},
		Servers: []OpenAPIServer{
			{URL: "https://api.example.com", Description: "production"},
			{URL: "http://localhost:8080", Description: "local development"},
		},
	}

	router := NewWithConfig(nil, WithOpenAPIInfo(info))

	spec, err := router.OpenAPIJSON()
	if err != nil {
		t.Fatalf("failed to marshal openapi json: %v", err)
	}

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(spec)
	if err != nil {
		t.Fatalf("failed to parse openapi json: %v", err)
	}

	if doc.Info == nil {
		t.Fatalf("expected info section to be present")
	}

	if doc.Info.Title != info.Title {
		t.Fatalf("expected title %q, got %q", info.Title, doc.Info.Title)
	}
	if doc.Info.Version != info.Version {
		t.Fatalf("expected version %q, got %q", info.Version, doc.Info.Version)
	}
	if doc.Info.Description != info.Description {
		t.Fatalf("expected description %q, got %q", info.Description, doc.Info.Description)
	}
	if doc.Info.TermsOfService != info.Terms {
		t.Fatalf("expected terms %q, got %q", info.Terms, doc.Info.TermsOfService)
	}

	if info.Contact == nil {
		t.Fatalf("test misconfigured: contact must be provided")
	}
	if doc.Info.Contact == nil || doc.Info.Contact.Name != info.Contact.Name || doc.Info.Contact.Email != info.Contact.Email || doc.Info.Contact.URL != info.Contact.URL {
		t.Fatalf("expected contact %+v, got %+v", info.Contact, doc.Info.Contact)
	}

	if info.License == nil {
		t.Fatalf("test misconfigured: license must be provided")
	}
	if doc.Info.License == nil || doc.Info.License.Name != info.License.Name || doc.Info.License.URL != info.License.URL {
		t.Fatalf("expected license %+v, got %+v", info.License, doc.Info.License)
	}

	if len(doc.Servers) != len(info.Servers) {
		t.Fatalf("expected %d servers, got %d", len(info.Servers), len(doc.Servers))
	}
	for i, server := range info.Servers {
		if doc.Servers[i] == nil {
			t.Fatalf("expected server entry at index %d", i)
		}
		if doc.Servers[i].URL != server.URL || doc.Servers[i].Description != server.Description {
			t.Fatalf("expected server %+v, got %+v", server, doc.Servers[i])
		}
	}
}

func pathKeys(paths *openapi3.Paths) []string {
	if paths == nil {
		return nil
	}
	m := paths.Map()
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
