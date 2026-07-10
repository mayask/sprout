package sprout

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

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

// --- Resolver and built-in schema tests ---

// testTimeResponse is a response struct exercising the built-in time.Time
// schema. Defined at package level so reflect.Type is stable.
type testTimeResponse struct {
	CreatedAt time.Time `json:"created_at"`
}

func TestTimeTimeGeneratesDateTimeSchema(t *testing.T) {
	router := New()

	GET(router, "/time", func(ctx context.Context, req *EmptyRequest) (*testTimeResponse, error) {
		return &testTimeResponse{CreatedAt: time.Now()}, nil
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

	respRef, ok := doc.Components.Schemas["sprout_testTimeResponse"]
	if !ok || respRef == nil {
		t.Fatalf("expected sprout_testTimeResponse component, got schemas %v", schemaKeys(doc.Components.Schemas))
	}

	createdRef := respRef.Value.Properties["created_at"]
	if createdRef == nil || createdRef.Value == nil {
		t.Fatalf("expected created_at property on testTimeResponse")
	}

	if !createdRef.Value.Type.Is("string") {
		t.Fatalf("expected created_at type string, got %v", createdRef.Value.Type)
	}
	if createdRef.Value.Format != "date-time" {
		t.Fatalf("expected created_at format date-time, got %q", createdRef.Value.Format)
	}

	if _, exists := doc.Components.Schemas["time_Time"]; exists {
		t.Fatalf("did not expect time_Time component to be generated")
	}
}

// testAmount is a struct-backed value object with an unexported field. Without
// a resolver it would emit an empty object schema.
type testAmount struct {
	value string
}

type testAmountResponse struct {
	Amount testAmount `json:"amount"`
}

func TestOpenAPISchemaResolverOverrideStructValueObject(t *testing.T) {
	amountType := reflect.TypeOf(testAmount{})

	router := NewWithConfig(nil,
		WithOpenAPISchemaResolver(func(t reflect.Type) *openapi3.SchemaRef {
			if t == amountType {
				return openapi3.NewStringSchema().NewRef()
			}
			return nil
		}),
	)

	GET(router, "/amount", func(ctx context.Context, req *EmptyRequest) (*testAmountResponse, error) {
		return &testAmountResponse{}, nil
	})

	specBytes, err := router.OpenAPIJSON()
	if err != nil {
		t.Fatalf("failed to marshal openapi json: %v", err)
	}

	// Use raw json.Unmarshal to inspect Ref without loader resolving $ref.
	var rawDoc openapi3.T
	if err := json.Unmarshal(specBytes, &rawDoc); err != nil {
		t.Fatalf("failed to unmarshal raw openapi json: %v", err)
	}

	respRef, ok := rawDoc.Components.Schemas["sprout_testAmountResponse"]
	if !ok || respRef == nil {
		t.Fatalf("expected sprout_testAmountResponse component, got schemas %v", schemaKeys(rawDoc.Components.Schemas))
	}

	// testAmount is a named type — resolver schema should be promoted to a component.
	amountComponentName := schemaComponentName(amountType)
	amountComp, ok := rawDoc.Components.Schemas[amountComponentName]
	if !ok {
		t.Fatalf("expected %s component for promoted resolver schema, got schemas %v", amountComponentName, schemaKeys(rawDoc.Components.Schemas))
	}
	if amountComp.Value == nil || !amountComp.Value.Type.Is("string") {
		t.Fatalf("expected %s to be type string, got %+v", amountComponentName, amountComp.Value)
	}

	// Property should be a $ref to the component, not an inline schema.
	amountRef := respRef.Value.Properties["amount"]
	if amountRef == nil {
		t.Fatalf("expected amount property on testAmountResponse")
	}
	if amountRef.Ref != "#/components/schemas/"+amountComponentName {
		t.Fatalf("expected amount property to be $ref to %s, got Ref=%q", amountComponentName, amountRef.Ref)
	}
}

// testStatus is a string-backed enum type. Without a resolver it would emit a
// plain string schema with no enum constraint.
type testStatus string

type testStatusResponse struct {
	Status testStatus `json:"status"`
}

func TestOpenAPISchemaResolverScalarEnum(t *testing.T) {
	statusType := reflect.TypeOf(testStatus(""))

	router := NewWithConfig(nil,
		WithOpenAPISchemaResolver(func(t reflect.Type) *openapi3.SchemaRef {
			if t == statusType {
				schema := openapi3.NewStringSchema()
				schema.Enum = []any{"active", "inactive"}
				return &openapi3.SchemaRef{Value: schema}
			}
			return nil
		}),
	)

	GET(router, "/status", func(ctx context.Context, req *EmptyRequest) (*testStatusResponse, error) {
		return &testStatusResponse{Status: "active"}, nil
	})

	specBytes, err := router.OpenAPIJSON()
	if err != nil {
		t.Fatalf("failed to marshal openapi json: %v", err)
	}

	// Use raw json.Unmarshal to inspect Ref without loader resolving $ref.
	var rawDoc openapi3.T
	if err := json.Unmarshal(specBytes, &rawDoc); err != nil {
		t.Fatalf("failed to unmarshal raw openapi json: %v", err)
	}

	respRef, ok := rawDoc.Components.Schemas["sprout_testStatusResponse"]
	if !ok || respRef == nil {
		t.Fatalf("expected sprout_testStatusResponse component, got schemas %v", schemaKeys(rawDoc.Components.Schemas))
	}

	// The enum schema should be promoted to a shared component, not inlined.
	statusComponentName := schemaComponentName(statusType)
	statusComp, ok := rawDoc.Components.Schemas[statusComponentName]
	if !ok {
		t.Fatalf("expected %s component for promoted enum schema, got schemas %v", statusComponentName, schemaKeys(rawDoc.Components.Schemas))
	}
	if statusComp.Value == nil || !statusComp.Value.Type.Is("string") {
		t.Fatalf("expected %s to be type string, got %+v", statusComponentName, statusComp.Value)
	}
	if len(statusComp.Value.Enum) != 2 {
		t.Fatalf("expected 2 enum values in component, got %d", len(statusComp.Value.Enum))
	}
	wantEnum := map[string]bool{"active": false, "inactive": false}
	for _, v := range statusComp.Value.Enum {
		if s, ok := v.(string); ok {
			wantEnum[s] = true
		}
	}
	for k, found := range wantEnum {
		if !found {
			t.Fatalf("expected enum value %q not found in component", k)
		}
	}

	// Property should be a $ref to the component, not an inline schema.
	statusRef := respRef.Value.Properties["status"]
	if statusRef == nil {
		t.Fatalf("expected status property on testStatusResponse")
	}
	if statusRef.Ref != "#/components/schemas/"+statusComponentName {
		t.Fatalf("expected status property to be $ref to %s, got Ref=%q", statusComponentName, statusRef.Ref)
	}
}

// testID is a string-backed type used to verify pointer/value normalization.
type testID string

type testIDResponse struct {
	ID    testID  `json:"id"`
	OptID *testID `json:"opt_id"`
}

func TestOpenAPISchemaResolverNormalizesPointerTypes(t *testing.T) {
	// A distinguishing format proves the resolver was consulted rather than
	// scalarSchemaRef's default plain-string output for type testID string.
	idType := reflect.TypeOf(testID(""))

	router := NewWithConfig(nil,
		WithOpenAPISchemaResolver(func(t reflect.Type) *openapi3.SchemaRef {
			if t == idType {
				schema := openapi3.NewStringSchema()
				schema.Format = "uuid"
				return &openapi3.SchemaRef{Value: schema}
			}
			return nil
		}),
	)

	GET(router, "/id", func(ctx context.Context, req *EmptyRequest) (*testIDResponse, error) {
		return &testIDResponse{ID: "abc"}, nil
	})

	specBytes, err := router.OpenAPIJSON()
	if err != nil {
		t.Fatalf("failed to marshal openapi json: %v", err)
	}

	// Use raw json.Unmarshal to inspect Ref without loader resolving $ref.
	var rawDoc openapi3.T
	if err := json.Unmarshal(specBytes, &rawDoc); err != nil {
		t.Fatalf("failed to unmarshal raw openapi json: %v", err)
	}

	respRef, ok := rawDoc.Components.Schemas["sprout_testIDResponse"]
	if !ok || respRef == nil {
		t.Fatalf("expected sprout_testIDResponse component, got schemas %v", schemaKeys(rawDoc.Components.Schemas))
	}

	// The resolver schema should be promoted to a shared component.
	idComponentName := schemaComponentName(idType)
	idComp, ok := rawDoc.Components.Schemas[idComponentName]
	if !ok {
		t.Fatalf("expected %s component, got schemas %v", idComponentName, schemaKeys(rawDoc.Components.Schemas))
	}
	if idComp.Value == nil || idComp.Value.Format != "uuid" {
		t.Fatalf("expected %s format uuid, got %+v", idComponentName, idComp.Value)
	}

	// Both fields should be $refs to the same component.
	for _, field := range []string{"id", "opt_id"} {
		fieldRef := respRef.Value.Properties[field]
		if fieldRef == nil {
			t.Fatalf("expected %s property on testIDResponse", field)
		}
		if fieldRef.Ref != "#/components/schemas/"+idComponentName {
			t.Fatalf("expected %s to be $ref to %s, got Ref=%q", field, idComponentName, fieldRef.Ref)
		}
	}
}

// testSharedEnum and response types using it prove that the same enum type
// used in multiple DTO components produces one shared component, not inline
// duplicates. Covers both scalar fields and array items.
type testSharedEnum string

type testSharedEnumResponseA struct {
	Value testSharedEnum `json:"value"`
}

type testSharedEnumResponseB struct {
	Value testSharedEnum `json:"value"`
}

type testSharedEnumSliceResponseA struct {
	Values []testSharedEnum `json:"values"`
}

type testSharedEnumSliceResponseB struct {
	Values []testSharedEnum `json:"values"`
}

func TestOpenAPISchemaResolverDeduplicatesEnumAcrossComponents(t *testing.T) {
	enumType := reflect.TypeOf(testSharedEnum(""))
	enumComponentName := schemaComponentName(enumType)
	wantRef := "#/components/schemas/" + enumComponentName

	router := NewWithConfig(nil,
		WithOpenAPISchemaResolver(func(t reflect.Type) *openapi3.SchemaRef {
			if t == enumType {
				schema := openapi3.NewStringSchema()
				schema.Enum = []any{"alpha", "beta", "gamma"}
				return &openapi3.SchemaRef{Value: schema}
			}
			return nil
		}),
	)

	GET(router, "/a", func(ctx context.Context, req *EmptyRequest) (*testSharedEnumResponseA, error) {
		return &testSharedEnumResponseA{}, nil
	})
	GET(router, "/b", func(ctx context.Context, req *EmptyRequest) (*testSharedEnumResponseB, error) {
		return &testSharedEnumResponseB{}, nil
	})
	GET(router, "/sa", func(ctx context.Context, req *EmptyRequest) (*testSharedEnumSliceResponseA, error) {
		return &testSharedEnumSliceResponseA{}, nil
	})
	GET(router, "/sb", func(ctx context.Context, req *EmptyRequest) (*testSharedEnumSliceResponseB, error) {
		return &testSharedEnumSliceResponseB{}, nil
	})

	specBytes, err := router.OpenAPIJSON()
	if err != nil {
		t.Fatalf("failed to marshal openapi json: %v", err)
	}

	// Use raw json.Unmarshal to inspect Ref without loader resolving $ref.
	var rawDoc openapi3.T
	if err := json.Unmarshal(specBytes, &rawDoc); err != nil {
		t.Fatalf("failed to unmarshal raw openapi json: %v", err)
	}

	// One shared component for the enum type.
	enumComp, ok := rawDoc.Components.Schemas[enumComponentName]
	if !ok {
		t.Fatalf("expected %s component, got schemas %v", enumComponentName, schemaKeys(rawDoc.Components.Schemas))
	}
	if enumComp.Value == nil || len(enumComp.Value.Enum) != 3 {
		t.Fatalf("expected 3 enum values in single component, got %+v", enumComp.Value)
	}

	// Scalar fields: both response components should $ref the same enum component.
	for _, respName := range []string{"sprout_testSharedEnumResponseA", "sprout_testSharedEnumResponseB"} {
		respRef, ok := rawDoc.Components.Schemas[respName]
		if !ok || respRef == nil {
			t.Fatalf("expected %s component, got schemas %v", respName, schemaKeys(rawDoc.Components.Schemas))
		}
		valueRef := respRef.Value.Properties["value"]
		if valueRef == nil {
			t.Fatalf("expected value property on %s", respName)
		}
		if valueRef.Ref != wantRef {
			t.Fatalf("expected %s.value to be $ref to %s, got Ref=%q", respName, enumComponentName, valueRef.Ref)
		}
	}

	// Array items: both slice response components should $ref the same enum component.
	for _, respName := range []string{"sprout_testSharedEnumSliceResponseA", "sprout_testSharedEnumSliceResponseB"} {
		respRef, ok := rawDoc.Components.Schemas[respName]
		if !ok || respRef == nil {
			t.Fatalf("expected %s component, got schemas %v", respName, schemaKeys(rawDoc.Components.Schemas))
		}
		valuesRef := respRef.Value.Properties["values"]
		if valuesRef == nil || valuesRef.Value == nil {
			t.Fatalf("expected values property on %s", respName)
		}
		if !valuesRef.Value.Type.Is("array") {
			t.Fatalf("expected %s.values to be array, got %v", respName, valuesRef.Value.Type)
		}
		if valuesRef.Value.Items == nil {
			t.Fatalf("expected %s.values to have items", respName)
		}
		if valuesRef.Value.Items.Ref != wantRef {
			t.Fatalf("expected %s.values.items to be $ref to %s, got Ref=%q", respName, enumComponentName, valuesRef.Value.Items.Ref)
		}
	}
}

// --- Embedded struct flattening tests ---

// testEmbeddedBase is a base struct with JSON fields, embedded anonymously
// by wrapper types that add an http status tag.
type testEmbeddedBase struct {
	Code    string `json:"code" validate:"required"`
	Message string `json:"message" validate:"required"`
}

// testEmbeddedWrapper embeds testEmbeddedBase with an http tag.
// The http tag causes shouldExcludeFromJSON to skip the field, but the
// embedded fields should still be flattened into the OpenAPI schema.
type testEmbeddedWrapper struct {
	testEmbeddedBase `http:"status=201"`
	Extra            string `json:"extra"`
}

func TestEmbeddedStructFieldsFlattenedInSchema(t *testing.T) {
	router := New()

	GET(router, "/embedded", func(ctx context.Context, req *EmptyRequest) (*testEmbeddedWrapper, error) {
		return &testEmbeddedWrapper{
			testEmbeddedBase: testEmbeddedBase{Code: "OK", Message: "done"},
			Extra:            "value",
		}, nil
	})

	specBytes, err := router.OpenAPIJSON()
	if err != nil {
		t.Fatalf("failed to marshal openapi json: %v", err)
	}

	// Use raw json.Unmarshal to inspect the schema without loader resolving $ref.
	var rawDoc openapi3.T
	if err := json.Unmarshal(specBytes, &rawDoc); err != nil {
		t.Fatalf("failed to unmarshal raw openapi json: %v", err)
	}

	respRef, ok := rawDoc.Components.Schemas["sprout_testEmbeddedWrapper"]
	if !ok || respRef == nil {
		t.Fatalf("expected sprout_testEmbeddedWrapper component, got schemas %v", schemaKeys(rawDoc.Components.Schemas))
	}

	if respRef.Value == nil {
		t.Fatalf("expected sprout_testEmbeddedWrapper to have a schema value")
	}

	// The embedded base fields (code, message) should be flattened into the wrapper.
	for _, field := range []string{"code", "message", "extra"} {
		prop, exists := respRef.Value.Properties[field]
		if !exists || prop == nil {
			t.Fatalf("expected property %q on testEmbeddedWrapper (flattened from embedded struct), got properties %v", field, respRef.Value.Properties)
		}
	}

	// code and message have validate:"required" — they should be in Required.
	required := make(map[string]bool)
	for _, r := range respRef.Value.Required {
		required[r] = true
	}
	if !required["code"] {
		t.Fatalf("expected 'code' in required fields, got %v", respRef.Value.Required)
	}
	if !required["message"] {
		t.Fatalf("expected 'message' in required fields, got %v", respRef.Value.Required)
	}
}

// testEmbeddedNoJSONTag embeds a struct without an http tag but also
// without a json tag. The fields should still be flattened.
type testEmbeddedNoJSONTag struct {
	testEmbeddedBase
	Label string `json:"label"`
}

func TestEmbeddedStructWithoutHTTPTagFlattenedInSchema(t *testing.T) {
	router := New()

	GET(router, "/embedded-no-http", func(ctx context.Context, req *EmptyRequest) (*testEmbeddedNoJSONTag, error) {
		return &testEmbeddedNoJSONTag{
			testEmbeddedBase: testEmbeddedBase{Code: "OK", Message: "done"},
			Label:            "test",
		}, nil
	})

	specBytes, err := router.OpenAPIJSON()
	if err != nil {
		t.Fatalf("failed to marshal openapi json: %v", err)
	}

	var rawDoc openapi3.T
	if err := json.Unmarshal(specBytes, &rawDoc); err != nil {
		t.Fatalf("failed to unmarshal raw openapi json: %v", err)
	}

	respRef, ok := rawDoc.Components.Schemas["sprout_testEmbeddedNoJSONTag"]
	if !ok || respRef == nil {
		t.Fatalf("expected sprout_testEmbeddedNoJSONTag component, got schemas %v", schemaKeys(rawDoc.Components.Schemas))
	}

	if respRef.Value == nil {
		t.Fatalf("expected sprout_testEmbeddedNoJSONTag to have a schema value")
	}

	for _, field := range []string{"code", "message", "label"} {
		prop, exists := respRef.Value.Properties[field]
		if !exists || prop == nil {
			t.Fatalf("expected property %q on testEmbeddedNoJSONTag, got properties %v", field, respRef.Value.Properties)
		}
	}
}

// --- Embedded struct alias optimization tests ---

// testAliasBase is the base response with real fields.
type testAliasBase struct {
	ID     string `json:"id" validate:"required"`
	Status string `json:"status" validate:"required"`
}

// testAliasWrapper embeds testAliasBase with an http status tag and adds
// NO extra fields. It should produce a $ref to testAliasBase, not a new
// component with inlined fields.
type testAliasWrapper struct {
	testAliasBase `http:"status=201"`
}

// testAliasWrapperWithExtra embeds testAliasBase with an http status tag
// AND adds an extra field. It should produce its own component with
// flattened fields.
type testAliasWrapperWithExtra struct {
	testAliasBase `http:"status=201"`
	Note          string `json:"note"`
}

func TestEmbeddedStructAliasReusesBaseComponent(t *testing.T) {
	router := New()

	POST(router, "/alias", func(ctx context.Context, req *EmptyRequest) (*testAliasWrapper, error) {
		return &testAliasWrapper{testAliasBase: testAliasBase{ID: "1", Status: "ok"}}, nil
	})

	specBytes, err := router.OpenAPIJSON()
	if err != nil {
		t.Fatalf("failed to marshal openapi json: %v", err)
	}

	var rawDoc openapi3.T
	if err := json.Unmarshal(specBytes, &rawDoc); err != nil {
		t.Fatalf("failed to unmarshal raw openapi json: %v", err)
	}

	// The wrapper should NOT have its own component — it should reuse the base.
	wrapperName := schemaComponentName(reflect.TypeOf(testAliasWrapper{}))
	if _, exists := rawDoc.Components.Schemas[wrapperName]; exists {
		t.Fatalf("did not expect %s component (should reuse base), got schemas %v", wrapperName, schemaKeys(rawDoc.Components.Schemas))
	}

	// The base type should have a component.
	baseName := schemaComponentName(reflect.TypeOf(testAliasBase{}))
	baseRef, ok := rawDoc.Components.Schemas[baseName]
	if !ok || baseRef == nil {
		t.Fatalf("expected %s component, got schemas %v", baseName, schemaKeys(rawDoc.Components.Schemas))
	}
	if baseRef.Value == nil || len(baseRef.Value.Properties) != 2 {
		t.Fatalf("expected base component with 2 properties, got %+v", baseRef.Value)
	}

	// The POST response schema should be a $ref to the base component.
	pathItem := rawDoc.Paths.Value("/alias")
	if pathItem == nil || pathItem.Post == nil {
		t.Fatalf("expected POST /alias in spec")
	}
	resp := pathItem.Post.Responses.Value("201")
	if resp == nil || resp.Value == nil {
		t.Fatalf("expected 201 response in spec")
	}
	media := resp.Value.Content["application/json"]
	if media == nil || media.Schema == nil {
		t.Fatalf("expected application/json schema in 201 response")
	}
	if media.Schema.Ref != "#/components/schemas/"+baseName {
		t.Fatalf("expected response schema to be $ref to %s, got Ref=%q", baseName, media.Schema.Ref)
	}
}

func TestEmbeddedStructAliasWithExtraFieldsCreatesComponent(t *testing.T) {
	router := New()

	POST(router, "/alias-extra", func(ctx context.Context, req *EmptyRequest) (*testAliasWrapperWithExtra, error) {
		return &testAliasWrapperWithExtra{testAliasBase: testAliasBase{ID: "1", Status: "ok"}, Note: "extra"}, nil
	})

	specBytes, err := router.OpenAPIJSON()
	if err != nil {
		t.Fatalf("failed to marshal openapi json: %v", err)
	}

	var rawDoc openapi3.T
	if err := json.Unmarshal(specBytes, &rawDoc); err != nil {
		t.Fatalf("failed to unmarshal raw openapi json: %v", err)
	}

	// The wrapper with extra fields SHOULD have its own component.
	wrapperName := schemaComponentName(reflect.TypeOf(testAliasWrapperWithExtra{}))
	wrapperRef, ok := rawDoc.Components.Schemas[wrapperName]
	if !ok || wrapperRef == nil {
		t.Fatalf("expected %s component (has extra fields), got schemas %v", wrapperName, schemaKeys(rawDoc.Components.Schemas))
	}

	// It should have flattened fields from the base plus the extra field.
	for _, field := range []string{"id", "status", "note"} {
		prop, exists := wrapperRef.Value.Properties[field]
		if !exists || prop == nil {
			t.Fatalf("expected property %q on %s, got properties %v", field, wrapperName, wrapperRef.Value.Properties)
		}
	}
}

// testAliasErrorBase is the base error type with code/message fields.
type testAliasErrorBase struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// testAliasBadRequestError embeds the base with a 4xx status tag and no
// extra fields. Despite being an embedded-only wrapper, it should keep its
// own component so Orval can generate distinct error types for catch handlers.
type testAliasBadRequestError struct {
	testAliasErrorBase `http:"status=400"`
}

// testAliasConflictError is another error wrapper with a different status.
type testAliasConflictError struct {
	testAliasErrorBase `http:"status=409"`
}

func (e *testAliasBadRequestError) Error() string { return e.Message }
func (e *testAliasConflictError) Error() string   { return e.Message }

func TestEmbeddedStructAliasPreservesErrorTypeComponents(t *testing.T) {
	router := New()

	POST(router, "/error-alias", func(ctx context.Context, req *EmptyRequest) (*testAliasBase, error) {
		return nil, &testAliasBadRequestError{testAliasErrorBase: testAliasErrorBase{Code: "bad_request", Message: "invalid"}}
	}, WithErrors(&testAliasBadRequestError{}, &testAliasConflictError{}))

	specBytes, err := router.OpenAPIJSON()
	if err != nil {
		t.Fatalf("failed to marshal openapi json: %v", err)
	}

	var rawDoc openapi3.T
	if err := json.Unmarshal(specBytes, &rawDoc); err != nil {
		t.Fatalf("failed to unmarshal raw openapi json: %v", err)
	}

	// Both error types should have their own components, not collapsed into the base.
	for _, typ := range []reflect.Type{reflect.TypeOf(testAliasBadRequestError{}), reflect.TypeOf(testAliasConflictError{})} {
		name := schemaComponentName(typ)
		ref, ok := rawDoc.Components.Schemas[name]
		if !ok || ref == nil {
			t.Fatalf("expected %s component, got schemas %v", name, schemaKeys(rawDoc.Components.Schemas))
		}
		if _, exists := ref.Value.Properties["code"]; !exists {
			t.Fatalf("expected 'code' property on %s", name)
		}
		if _, exists := ref.Value.Properties["message"]; !exists {
			t.Fatalf("expected 'message' property on %s", name)
		}
	}
}

func schemaKeys(schemas openapi3.Schemas) []string {
	keys := make([]string, 0, len(schemas))
	for k := range schemas {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
