package sprout

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

type securityTestResponse struct {
	OK bool `json:"ok"`
}

func securityOK(context.Context, *EmptyRequest) (*securityTestResponse, error) {
	return &securityTestResponse{OK: true}, nil
}

func clientCredentialsScheme() *openapi3.SecurityScheme {
	return &openapi3.SecurityScheme{
		Type:        "oauth2",
		Description: "Machine-to-machine access",
		Extensions:  map[string]any{"x-scalar-client-id": "demo"},
		Flows: &openapi3.OAuthFlows{
			ClientCredentials: &openapi3.OAuthFlow{
				TokenURL:   "https://api.example.com/business/v1/auth/token",
				Scopes:     map[string]string{"payments:view": "View payments", "wallets:view": "View wallets"},
				Extensions: map[string]any{"x-default-scopes": []string{"wallets:view"}},
			},
		},
	}
}

func bearerScheme() *openapi3.SecurityScheme {
	return &openapi3.SecurityScheme{Type: "http", Scheme: "bearer", BearerFormat: "JWT", Description: "Access token"}
}

// newBusinessRouter mirrors the backend layout: a public token route before
// the security default, secured routes after it, and nested mounts.
func newBusinessRouter(t *testing.T) (root, business *Sprout, events *[]string) {
	t.Helper()
	events = &[]string{}
	root = NewWithConfig(nil, WithOpenAPIInfo(OpenAPIInfo{Title: "Internal"}))
	GET(root, "/api/v1/ping", securityOK)

	business = root.Mount("/business/v1", nil,
		WithOpenAPIDocument(OpenAPIInfo{Title: "Business", Servers: []OpenAPIServer{{URL: "https://api.example.com/business/v1"}}}),
		WithOpenAPIRelativePaths(),
		WithOpenAPISecurityScheme("oauth2", clientCredentialsScheme()),
		WithOpenAPISecurityScheme("bearer", bearerScheme()),
		WithOpenAPISecurity("oauth2"),
	)
	POST(business, "/auth/token", securityOK)
	business.Use(func(w http.ResponseWriter, r *http.Request, next Next) {
		*events = append(*events, "auth")
		next(nil)
	})
	business.UseOpenAPISecurity("oauth2")
	GET(business, "/me", securityOK)
	GET(business, "/status", securityOK, WithoutSecurity())
	GET(business, "", securityOK)

	requirePayments := RouteOptions(
		WithMiddleware(func(w http.ResponseWriter, r *http.Request, next Next) {
			*events = append(*events, "acl")
			next(nil)
		}),
		WithSecurity("oauth2", "payments:view"),
	)
	payments := business.Mount("/payments", nil)
	GET(payments, "/", securityOK, requirePayments)
	GET(payments, "/:id", securityOK, WithSecurity("oauth2", "payments:view"), WithSecurity("bearer"))
	return root, business, events
}

func TestOpenAPIRelativePathsAndSecurity(t *testing.T) {
	root, business, events := newBusinessRouter(t)
	doc := exportDoc(t, business)

	if got, want := pathKeys(doc.Paths), []string{"/", "/auth/token", "/me", "/payments/", "/payments/{id}", "/status"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("business paths = %v, want %v", got, want)
	}
	if got := pathKeys(exportDoc(t, root).Paths); !reflect.DeepEqual(got, []string{"/api/v1/ping"}) {
		t.Fatalf("root paths = %v", got)
	}

	type opWant struct {
		path, method, id string
		security         *openapi3.SecurityRequirements // nil: inherits the top level
	}
	public := &openapi3.SecurityRequirements{}
	for _, w := range []opWant{
		{"/auth/token", http.MethodPost, "postAuthToken", public},
		{"/me", http.MethodGet, "getMe", nil},
		{"/status", http.MethodGet, "getStatus", public},
		{"/", http.MethodGet, "get", nil},
		{"/payments/", http.MethodGet, "getPayments", &openapi3.SecurityRequirements{{"oauth2": {"payments:view"}}}},
		{"/payments/{id}", http.MethodGet, "getPaymentsId", &openapi3.SecurityRequirements{{"oauth2": {"payments:view"}}, {"bearer": {}}}},
	} {
		op := doc.Paths.Value(w.path).GetOperation(w.method)
		if op.OperationID != w.id {
			t.Fatalf("%s %s operationId = %q, want %q", w.method, w.path, op.OperationID, w.id)
		}
		if !reflect.DeepEqual(op.Security, w.security) {
			t.Fatalf("%s %s security = %+v, want %+v", w.method, w.path, op.Security, w.security)
		}
	}
	if !reflect.DeepEqual(doc.Security, openapi3.SecurityRequirements{{"oauth2": {}}}) {
		t.Fatalf("top-level security = %+v", doc.Security)
	}

	yaml, err := business.OpenAPIYAML()
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"tokenUrl: https://api.example.com/business/v1/auth/token",
		"x-scalar-client-id: demo",
		"x-default-scopes:",
		"bearerFormat: JWT",
		"security: []",
		"- oauth2: []",
	} {
		if !strings.Contains(string(yaml), fragment) {
			t.Fatalf("business YAML lacks %q:\n%s", fragment, yaml)
		}
	}
	rootYAML, _ := root.OpenAPIYAML()
	if strings.Contains(string(rootYAML), "securitySchemes") || strings.Contains(string(rootYAML), "security:") {
		t.Fatalf("security leaked into the root document:\n%s", rootYAML)
	}

	// Routing still uses the full mounted paths and the combined options.
	for _, tc := range []struct {
		path   string
		events []string
	}{
		{"/business/v1/payments/", []string{"auth", "acl"}},
		{"/business/v1/me", []string{"auth"}},
	} {
		*events = nil
		rec := httptest.NewRecorder()
		root.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != http.StatusOK || !reflect.DeepEqual(*events, tc.events) {
			t.Fatalf("%s: status %d events %v, want 200 %v", tc.path, rec.Code, *events, tc.events)
		}
	}
}

func TestOpenAPISecurityWithoutDocumentDefault(t *testing.T) {
	router := NewWithConfig(nil, WithOpenAPISecurityScheme("bearer", bearerScheme()))
	GET(router, "/public", securityOK)
	router.UseOpenAPISecurity("bearer")
	GET(router, "/private", securityOK)
	GET(router, "/open", securityOK, WithoutSecurity())

	child := router.Mount("/child", nil, WithOpenAPIDocument(OpenAPIInfo{Title: "Child"}))
	GET(child, "/route", securityOK) // parent default stops at the document boundary

	doc := exportDoc(t, router)
	if doc.Security != nil {
		t.Fatalf("unexpected top-level security %+v", doc.Security)
	}
	for path, want := range map[string]*openapi3.SecurityRequirements{
		"/public":  nil,
		"/open":    nil,
		"/private": {{"bearer": {}}},
	} {
		if got := doc.Paths.Value(path).Get.Security; !reflect.DeepEqual(got, want) {
			t.Fatalf("%s security = %+v, want %+v", path, got, want)
		}
	}
	if got := exportDoc(t, child).Paths.Value("/child/route").Get.Security; got != nil {
		t.Fatalf("child route inherited security across documents: %+v", got)
	}
}

func TestOpenAPISecurityAndPathMisconfigurationPanics(t *testing.T) {
	withSchemes := func() *Sprout {
		return NewWithConfig(nil,
			WithOpenAPISecurityScheme("oauth2", clientCredentialsScheme()),
			WithOpenAPISecurityScheme("bearer", bearerScheme()),
		)
	}
	for name, tc := range map[string]struct {
		fn   func()
		want string
	}{
		"unknown scheme on route": {func() { GET(withSchemes(), "/x", securityOK, WithSecurity("apiKey")) }, `unknown OpenAPI security scheme "apiKey"`},
		"unknown scope on route":  {func() { GET(withSchemes(), "/x", securityOK, WithSecurity("oauth2", "payments:veiw")) }, `does not declare scope "payments:veiw"`},
		"scopes on bearer":        {func() { GET(withSchemes(), "/x", securityOK, WithSecurity("bearer", "payments:view")) }, "does not take scopes"},
		"unknown router default":  {func() { withSchemes().UseOpenAPISecurity("apiKey") }, `unknown OpenAPI security scheme "apiKey"`},
		"unknown document default": {func() {
			New().Mount("/b", nil, WithOpenAPIDocument(OpenAPIInfo{}), WithOpenAPISecurity("oauth2"))
		}, `unknown OpenAPI security scheme "oauth2"`},
		"WithSecurity and WithoutSecurity": {func() {
			GET(withSchemes(), "/x", securityOK, WithSecurity("bearer"), WithoutSecurity())
		}, "cannot be combined"},
		"security scheme without document": {func() {
			New().Mount("/b", nil, WithOpenAPISecurityScheme("bearer", bearerScheme()))
		}, "require WithOpenAPIDocument"},
		"relative paths without document": {func() { New().Mount("/b", nil, WithOpenAPIRelativePaths()) }, "require WithOpenAPIDocument"},
		"relative paths on root without document": {func() {
			NewWithConfig(&Config{BasePath: "/api"}, WithOpenAPIRelativePaths())
		}, "WithOpenAPIRelativePaths requires WithOpenAPIDocument"},
		"duplicate operationId": {func() {
			r := New()
			GET(r, "/items/:id", securityOK)
			GET(r, "/items/id", securityOK)
		}, `operationId "getItemsId" of GET /items/id duplicates GET /items/{id}`},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				recovered := recover()
				if msg := fmt.Sprint(recovered); recovered == nil || !strings.Contains(msg, tc.want) {
					t.Fatalf("panic = %v, want message containing %q", recovered, tc.want)
				}
			}()
			tc.fn()
		})
	}
}

func TestOpenAPIOperationIDsAreUniquePerDocument(t *testing.T) {
	root := New()
	GET(root, "/me", securityOK)
	business := root.Mount("/business/v1", nil, WithOpenAPIDocument(OpenAPIInfo{}), WithOpenAPIRelativePaths())
	GET(business, "/me", securityOK) // same operationId, different document

	if a, b := exportDoc(t, root).Paths.Value("/me").Get.OperationID, exportDoc(t, business).Paths.Value("/me").Get.OperationID; a != "getMe" || b != "getMe" {
		t.Fatalf("operationIds = %q, %q", a, b)
	}
}
