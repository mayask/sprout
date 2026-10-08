package sprout

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	alphapayments "github.com/mayask/sprout/internal/openapitest/a/payments"
	betapayments "github.com/mayask/sprout/internal/openapitest/b/payments"
	nestedpayments "github.com/mayask/sprout/internal/openapitest/x/a/payments"
)

type internalOnlyResponse struct {
	Secret string `json:"secret"`
}

type businessWalletResponse struct {
	ID      string             `json:"id"`
	Balance businessMoneyValue `json:"balance"`
}

type businessMoneyValue struct {
	Amount string `json:"amount"`
}

type businessTokenResponse struct {
	Token string `json:"token"`
}

type sharedPingResponse struct {
	OK bool `json:"ok"`
}

type isolatedEnum string

type isolatedEnumResponse struct {
	Value isolatedEnum `json:"value"`
}

func enumResolver(format string) OpenAPISchemaResolver {
	return func(t reflect.Type) *openapi3.SchemaRef {
		if t == reflect.TypeOf(isolatedEnum("")) {
			schema := openapi3.NewStringSchema()
			schema.Format = format
			return schema.NewRef()
		}
		return nil
	}
}

func TestMountWithOpenAPIDocumentIsolatesDocuments(t *testing.T) {
	var events []string
	router := NewWithConfig(&Config{
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			var sproutErr *Error
			if errors.As(err, &sproutErr) {
				events = append(events, "error:"+string(sproutErr.Kind))
			}
			w.WriteHeader(599)
		},
	}, WithOpenAPIInfo(OpenAPIInfo{Title: "Internal API", Version: "1.12.0"}))
	router.Use(func(w http.ResponseWriter, r *http.Request, next Next) {
		events = append(events, "root-mw")
		next(nil)
	})

	api := router.Mount("/api/v1", nil)
	GET(api, "/secrets", func(context.Context, *EmptyRequest) (*internalOnlyResponse, error) {
		return &internalOnlyResponse{}, nil
	})

	business := router.Mount("/business/v1", nil, WithOpenAPIDocument(OpenAPIInfo{
		Title:       "Business API",
		Version:     "1.0.0",
		Description: "Public business API",
	}))
	POST(business, "/token", func(context.Context, *EmptyRequest) (*businessTokenResponse, error) {
		return &businessTokenResponse{Token: "t"}, nil
	})
	business.Use(func(w http.ResponseWriter, r *http.Request, next Next) {
		events = append(events, "business-auth")
		next(nil)
	})
	wallets := business.Mount("/wallets", nil)
	GET(wallets, "/:id", func(context.Context, *EmptyRequest) (*businessWalletResponse, error) {
		return &businessWalletResponse{ID: "w"}, nil
	})

	// Registered after the business mount: must still land in the root document.
	backoffice := router.Mount("/backoffice/v1", nil)
	GET(backoffice, "/ping", func(context.Context, *EmptyRequest) (*sharedPingResponse, error) {
		return &sharedPingResponse{OK: true}, nil
	})

	rootDoc := exportDoc(t, router)
	businessDoc := exportDoc(t, business)

	if got, want := pathKeys(rootDoc.Paths), []string{"/api/v1/secrets", "/backoffice/v1/ping"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("root paths = %v, want %v", got, want)
	}
	if got, want := pathKeys(businessDoc.Paths), []string{"/business/v1/token", "/business/v1/wallets/{id}"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("business paths = %v, want %v", got, want)
	}
	if got := pathKeys(exportDoc(t, wallets).Paths); !reflect.DeepEqual(got, pathKeys(businessDoc.Paths)) {
		t.Fatalf("nested mount should export the business document, got paths %v", got)
	}

	if rootDoc.Info.Title != "Internal API" || rootDoc.Info.Version != "1.12.0" {
		t.Fatalf("root info changed: %+v", rootDoc.Info)
	}
	if businessDoc.Info.Title != "Business API" || businessDoc.Info.Version != "1.0.0" || businessDoc.Info.Description != "Public business API" {
		t.Fatalf("business info = %+v", businessDoc.Info)
	}

	if got, want := schemaKeys(rootDoc.Components.Schemas), []string{"sprout_internalOnlyResponse", "sprout_sharedPingResponse"}; !reflect.DeepEqual(sorted(got), want) {
		t.Fatalf("root components = %v, want %v", sorted(got), want)
	}
	if got, want := schemaKeys(businessDoc.Components.Schemas), []string{"sprout_businessMoneyValue", "sprout_businessTokenResponse", "sprout_businessWalletResponse"}; !reflect.DeepEqual(sorted(got), want) {
		t.Fatalf("business components = %v, want %v", sorted(got), want)
	}
	assertRefClosure(t, router)
	assertRefClosure(t, business)

	// Routing is unchanged: inherited middleware, auth ordering, and fallbacks.
	for _, tc := range []struct {
		method, path string
		status       int
		events       []string
	}{
		{http.MethodPost, "/business/v1/token", http.StatusOK, []string{"root-mw"}},
		{http.MethodGet, "/business/v1/wallets/w1", http.StatusOK, []string{"root-mw", "business-auth"}},
		{http.MethodGet, "/api/v1/secrets", http.StatusOK, []string{"root-mw"}},
		{http.MethodGet, "/business/v1/missing", 599, []string{"root-mw", "business-auth", "error:not_found"}},
		{http.MethodDelete, "/business/v1/wallets/w1", 599, []string{"root-mw", "business-auth", "error:method_not_allowed"}},
	} {
		events = nil
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != tc.status || !reflect.DeepEqual(events, tc.events) {
			t.Fatalf("%s %s: status %d events %v, want %d %v", tc.method, tc.path, rec.Code, events, tc.status, tc.events)
		}
	}

	// The business document is not served automatically.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/business/v1/swagger", nil))
	if rec.Code != 599 {
		t.Fatalf("expected /business/v1/swagger to be unrouted, got %d", rec.Code)
	}
}

func TestMountWithOpenAPIDocumentResolverIsCopiedFromParent(t *testing.T) {
	router := NewWithConfig(nil, WithOpenAPISchemaResolver(enumResolver("root")))

	inherited := router.Mount("/inherited", nil, WithOpenAPIDocument(OpenAPIInfo{Title: "Inherited"}))
	overridden := router.Mount("/overridden", nil,
		WithOpenAPIDocument(OpenAPIInfo{Title: "Overridden"}),
		WithOpenAPISchemaResolver(enumResolver("override")),
	)

	// Later changes are not shared between documents in either direction.
	router.RegisterOpenAPISchemaResolver(enumResolver("root-late"))
	overridden.RegisterOpenAPISchemaResolver(enumResolver("override-late"))

	for _, r := range []*Sprout{router, inherited, overridden} {
		GET(r, "/enum", func(context.Context, *EmptyRequest) (*isolatedEnumResponse, error) {
			return &isolatedEnumResponse{}, nil
		})
	}

	name := schemaComponentName(reflect.TypeOf(isolatedEnum("")))
	for _, tc := range []struct {
		router *Sprout
		format string
	}{
		{router, "root-late"},
		{inherited, "root"},
		{overridden, "override-late"},
	} {
		doc := exportDoc(t, tc.router)
		comp := doc.Components.Schemas[name]
		if comp == nil || comp.Value == nil || comp.Value.Format != tc.format {
			t.Fatalf("%s: enum component = %+v, want format %q", doc.Info.Title, comp, tc.format)
		}
	}
}

func TestMountOpenAPIOptionsWithoutDocumentPanic(t *testing.T) {
	for name, opt := range map[string]Option{
		"info":     WithOpenAPIInfo(OpenAPIInfo{Title: "x"}),
		"resolver": WithOpenAPISchemaResolver(enumResolver("x")),
	} {
		t.Run(name, func(t *testing.T) {
			router := New()
			defer func() {
				if recover() == nil {
					t.Fatal("expected panic")
				}
			}()
			router.Mount("/child", nil, opt)
		})
	}
}

func TestMountIgnoresOpenAPIOptionsCarriedByReusedConfig(t *testing.T) {
	cfg := &Config{}
	router := NewWithConfig(cfg, WithOpenAPIInfo(OpenAPIInfo{Title: "Root"}))
	child := router.Mount("/child", cfg)
	GET(child, "/ping", func(context.Context, *EmptyRequest) (*sharedPingResponse, error) {
		return &sharedPingResponse{}, nil
	})

	if got := pathKeys(exportDoc(t, router).Paths); !reflect.DeepEqual(got, []string{"/child/ping"}) {
		t.Fatalf("child should share the root document, root paths = %v", got)
	}
}

func TestOpenAPIComponentNamesDisambiguateSamePackageName(t *testing.T) {
	router := New()
	// Registered first, while its default name is still unambiguous.
	GET(router, "/alpha", func(context.Context, *EmptyRequest) (*alphapayments.PaymentResponse, error) {
		return &alphapayments.PaymentResponse{}, nil
	})
	GET(router, "/beta", func(context.Context, *EmptyRequest) (*betapayments.PaymentResponse, error) {
		return &betapayments.PaymentResponse{}, nil
	})
	GET(router, "/nested", func(context.Context, *EmptyRequest) (*nestedpayments.PaymentResponse, error) {
		return &nestedpayments.PaymentResponse{}, nil
	})
	GET(router, "/ping", func(context.Context, *EmptyRequest) (*sharedPingResponse, error) {
		return &sharedPingResponse{}, nil
	})

	doc := exportDoc(t, router)

	// Only colliding types move deeper, and only as far as needed:
	// a/payments vs x/a/payments still collide at depth 2.
	want := map[string]string{
		"/alpha":  "openapitest_a_payments_PaymentResponse",
		"/beta":   "b_payments_PaymentResponse",
		"/nested": "x_a_payments_PaymentResponse",
		"/ping":   "sprout_sharedPingResponse",
	}
	wantField := map[string]string{
		"/alpha":  "alpha_reference",
		"/beta":   "beta_reference",
		"/nested": "nested_reference",
	}
	for path, component := range want {
		ref := doc.Paths.Value(path).Get.Responses.Value("200").Value.Content["application/json"].Schema.Ref
		if ref != "#/components/schemas/"+component {
			t.Fatalf("%s response ref = %q, want component %s", path, ref, component)
		}
		if field := wantField[path]; field != "" {
			if _, ok := doc.Components.Schemas[component].Value.Properties[field]; !ok {
				t.Fatalf("component %s lost its own schema (missing %q)", component, field)
			}
		}
	}
	if _, ok := doc.Components.Schemas["payments_PaymentResponse"]; ok {
		t.Fatal("ambiguous payments_PaymentResponse component should not exist")
	}
	assertRefClosure(t, router)
}

func TestOpenAPIComponentNamesIndependentOfRegistrationOrder(t *testing.T) {
	register := []func(*Sprout){
		func(r *Sprout) {
			GET(r, "/alpha", func(context.Context, *EmptyRequest) (*alphapayments.PaymentResponse, error) { return nil, nil })
		},
		func(r *Sprout) {
			GET(r, "/beta", func(context.Context, *EmptyRequest) (*betapayments.PaymentResponse, error) { return nil, nil })
		},
		func(r *Sprout) {
			GET(r, "/nested", func(context.Context, *EmptyRequest) (*nestedpayments.PaymentResponse, error) { return nil, nil })
		},
	}

	var specs []string
	for _, order := range [][]int{{0, 1, 2}, {2, 1, 0}, {1, 2, 0}} {
		router := New()
		for _, i := range order {
			register[i](router)
		}
		spec, err := router.OpenAPIYAML()
		if err != nil {
			t.Fatal(err)
		}
		specs = append(specs, string(spec))
	}
	for i := 1; i < len(specs); i++ {
		if specs[i] != specs[0] {
			t.Fatalf("spec differs by registration order:\n%s\n---\n%s", specs[0], specs[i])
		}
	}
}

func TestOpenAPIExplicitResponseBodyDoesNotLeaveWrapperComponent(t *testing.T) {
	router := New()
	GET(router, "/download", func(context.Context, *fileBodyRequest) (*fileBodyResponse, error) {
		return nil, nil
	})

	doc := exportDoc(t, router)
	if _, ok := doc.Components.Schemas[schemaComponentName(reflect.TypeOf(fileBodyResponse{}))]; ok {
		t.Fatalf("unexpected wrapper component, got %v", schemaKeys(doc.Components.Schemas))
	}
	assertRefClosure(t, router)
}

func TestOpenAPISchemaResolverReturningRefPanics(t *testing.T) {
	router := NewWithConfig(nil, WithOpenAPISchemaResolver(func(t reflect.Type) *openapi3.SchemaRef {
		if t == reflect.TypeOf(isolatedEnum("")) {
			schema := openapi3.NewArraySchema()
			schema.Items = openapi3.NewSchemaRef("#/components/schemas/Elsewhere", nil)
			return schema.NewRef()
		}
		return nil
	}))
	defer func() {
		if recovered := recover(); recovered == nil || !strings.Contains(recovered.(string), "inline schemas") {
			t.Fatalf("expected inline-schema panic, got %v", recovered)
		}
	}()
	GET(router, "/enum", func(context.Context, *EmptyRequest) (*isolatedEnumResponse, error) { return nil, nil })
}

func exportDoc(t *testing.T, r *Sprout) *openapi3.T {
	t.Helper()
	data, err := r.OpenAPIJSON()
	if err != nil {
		t.Fatalf("export openapi: %v", err)
	}
	var doc openapi3.T
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal openapi: %v", err)
	}
	return &doc
}

// assertRefClosure checks that every $ref resolves inside the exported
// document and that every component is reachable from a path.
func assertRefClosure(t *testing.T, r *Sprout) {
	t.Helper()
	data, err := r.OpenAPIJSON()
	if err != nil {
		t.Fatalf("export openapi: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal openapi: %v", err)
	}
	components, _ := raw["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)

	const prefix = "#/components/schemas/"
	reached := map[string]bool{}
	queue := collectRefs(raw["paths"])
	for len(queue) > 0 {
		ref := queue[0]
		queue = queue[1:]
		if !strings.HasPrefix(ref, prefix) {
			t.Fatalf("unexpected $ref %q", ref)
		}
		name := strings.TrimPrefix(ref, prefix)
		if reached[name] {
			continue
		}
		schema, ok := schemas[name]
		if !ok {
			t.Fatalf("$ref %q does not resolve; components %v", ref, sortedKeys(schemas))
		}
		reached[name] = true
		queue = append(queue, collectRefs(schema)...)
	}
	for name := range schemas {
		if !reached[name] {
			t.Fatalf("component %q is unreachable from any path", name)
		}
	}
}

func collectRefs(node any) []string {
	var refs []string
	switch v := node.(type) {
	case map[string]any:
		for key, child := range v {
			if s, ok := child.(string); ok && key == "$ref" {
				refs = append(refs, s)
				continue
			}
			refs = append(refs, collectRefs(child)...)
		}
	case []any:
		for _, child := range v {
			refs = append(refs, collectRefs(child)...)
		}
	}
	return refs
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
