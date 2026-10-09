package sprout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

type defaultTokenBody struct {
	GrantType    string `json:"grant_type" default:"client_credentials" validate:"eq=client_credentials"`
	ClientID     string `json:"client_id" validate:"required"`
	ClientSecret string `json:"client_secret" validate:"required"`
	defaultOptions
}

type defaultOptions struct {
	Audience string `json:"audience" default:"api"`
}

type defaultTokenRequest struct {
	Page  int              `query:"page" default:"1" validate:"required,min=1"`
	Sort  []string         `query:"sort" default:"created_at,id"`
	Limit *int             `query:"limit" default:"20"`
	Body  defaultTokenBody `body:"" contentType:"application/json,application/x-www-form-urlencoded"`
}

type defaultTokenResponse struct {
	Page         int      `json:"page"`
	Sort         []string `json:"sort"`
	Limit        int      `json:"limit"`
	GrantType    string   `json:"grant_type"`
	Audience     string   `json:"audience"`
	BodyAbsent   bool     `json:"body_absent"`
	PointerScope string   `json:"pointer_scope"`
}

type defaultPointerBodyRequest struct {
	Body *struct {
		Scope   string         `json:"scope" default:"read"`
		Options defaultOptions `json:"options"`
	} `body:""`
}

func newDefaultsRouter(t *testing.T, captured *error) *Sprout {
	t.Helper()
	router := NewWithConfig(&Config{
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			*captured = err
			w.WriteHeader(http.StatusBadRequest)
		},
	})
	POST(router, "/token", func(_ context.Context, req *defaultTokenRequest) (*defaultTokenResponse, error) {
		resp := &defaultTokenResponse{
			Page: req.Page, Sort: req.Sort, GrantType: req.Body.GrantType, Audience: req.Body.Audience,
		}
		if req.Limit != nil {
			resp.Limit = *req.Limit
		}
		resp.Sort = append([]string(nil), req.Sort...)
		req.Sort[0] = "mutated" // must not leak into later requests
		return resp, nil
	})
	POST(router, "/pointer", func(_ context.Context, req *defaultPointerBodyRequest) (*defaultTokenResponse, error) {
		if req.Body == nil {
			return &defaultTokenResponse{BodyAbsent: true}, nil
		}
		return &defaultTokenResponse{PointerScope: req.Body.Scope, Audience: req.Body.Options.Audience}, nil
	})
	return router
}

func callDefaults(t *testing.T, router *Sprout, target, contentType, body string) (int, defaultTokenResponse) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var resp defaultTokenResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v (%s)", err, rec.Body.String())
		}
	}
	return rec.Code, resp
}

func TestDefaultsApplyOnlyToAbsentValues(t *testing.T) {
	const creds = `"client_id":"id","client_secret":"s"`
	for _, tc := range []struct {
		name, target, contentType, body string
		want                            defaultTokenResponse
	}{
		{"absent everywhere", "/token", "application/json", `{` + creds + `}`,
			defaultTokenResponse{Page: 1, Sort: []string{"created_at", "id"}, Limit: 20, GrantType: "client_credentials", Audience: "api"}},
		{"present values win", "/token?page=3&sort=name&limit=5", "application/json",
			`{"grant_type":"client_credentials","audience":"admin",` + creds + `}`,
			defaultTokenResponse{Page: 3, Sort: []string{"name"}, Limit: 5, GrantType: "client_credentials", Audience: "admin"}},
		{"empty query values count as absent", "/token?page=&sort=", "application/json", `{` + creds + `}`,
			defaultTokenResponse{Page: 1, Sort: []string{"created_at", "id"}, Limit: 20, GrantType: "client_credentials", Audience: "api"}},
		{"JSON null keeps the default of a non-pointer field", "/token", "application/json", `{"grant_type":null,` + creds + `}`,
			defaultTokenResponse{Page: 1, Sort: []string{"created_at", "id"}, Limit: 20, GrantType: "client_credentials", Audience: "api"}},
		{"form body", "/token", formURLEncodedContentType, "client_id=id&client_secret=s&scope=x",
			defaultTokenResponse{Page: 1, Sort: []string{"created_at", "id"}, Limit: 20, GrantType: "client_credentials", Audience: "api"}},
		{"pointer body absent stays nil", "/pointer", "", "", defaultTokenResponse{BodyAbsent: true}},
		{"pointer body gets nested defaults when present", "/pointer", "application/json", `{}`, defaultTokenResponse{PointerScope: "read", Audience: "api"}},
		{"nested object keeps defaults for absent keys", "/pointer", "application/json", `{"scope":"write","options":{}}`, defaultTokenResponse{PointerScope: "write", Audience: "api"}},
		{"nested present value wins", "/pointer", "application/json", `{"options":{"audience":"admin"}}`, defaultTokenResponse{PointerScope: "read", Audience: "admin"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var captured error
			router := newDefaultsRouter(t, &captured)
			for range 2 { // the second call proves defaults are not shared
				status, got := callDefaults(t, router, tc.target, tc.contentType, tc.body)
				if status != http.StatusOK || !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("status %d err %v got %+v, want %+v", status, captured, got, tc.want)
				}
			}
		})
	}
}

func TestDefaultedFieldsAreValidatedWithRequestValues(t *testing.T) {
	var captured error
	router := newDefaultsRouter(t, &captured)
	for _, tc := range []struct {
		name, contentType, body string
		fields                  []string
	}{
		{"empty body reports only required fields", "", "", []string{"client_id", "client_secret"}},
		{"empty form reports only required fields", formURLEncodedContentType, "", []string{"client_id", "client_secret"}},
		{"explicit value is validated", "application/json", `{"grant_type":"password","client_id":"id","client_secret":"s"}`, []string{"grant_type"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			captured = nil
			status, _ := callDefaults(t, router, "/token", tc.contentType, tc.body)
			var sproutErr *Error
			if status != http.StatusBadRequest || !errors.As(captured, &sproutErr) {
				t.Fatalf("status %d err %v", status, captured)
			}
			var fields []string
			for _, f := range sproutErr.Fields {
				fields = append(fields, f.Field)
			}
			if !reflect.DeepEqual(fields, tc.fields) {
				t.Fatalf("fields %v, want %v", fields, tc.fields)
			}
		})
	}
}

func TestInvalidDefaultsPanicAtRegistration(t *testing.T) {
	ok := func(context.Context, *struct{}) (*securityTestResponse, error) { return nil, nil }
	_ = ok
	for name, tc := range map[string]struct {
		register func(*Sprout)
		want     string
	}{
		"unconvertible query default": {func(r *Sprout) {
			POST(r, "/x", func(context.Context, *struct {
				Page int `query:"page" default:"one"`
			}) (*securityTestResponse, error) {
				return nil, nil
			})
		}, "invalid default"},
		"unconvertible body default": {func(r *Sprout) {
			POST(r, "/x", func(context.Context, *struct {
				Body struct {
					TTL int `json:"ttl" default:"soon"`
				} `body:""`
			}) (*securityTestResponse, error) {
				return nil, nil
			})
		}, "invalid default"},
		"path default": {func(r *Sprout) {
			GET(r, "/x/:id", func(context.Context, *struct {
				ID string `path:"id" default:"a"`
			}) (*securityTestResponse, error) {
				return nil, nil
			})
		}, "query and body fields only"},
		"header default": {func(r *Sprout) {
			GET(r, "/x", func(context.Context, *struct {
				Trace string `header:"X-Trace" default:"a"`
			}) (*securityTestResponse, error) {
				return nil, nil
			})
		}, "query and body fields only"},
		"default inside slice elements": {func(r *Sprout) {
			POST(r, "/x", func(context.Context, *struct {
				Items []defaultOptions `json:"items"`
			}) (*securityTestResponse, error) {
				return nil, nil
			})
		}, "defaultOptions.Audience"},
		"default inside pointer struct": {func(r *Sprout) {
			POST(r, "/x", func(context.Context, *struct {
				Body struct {
					Options *defaultOptions `json:"options"`
				} `body:""`
			}) (*securityTestResponse, error) {
				return nil, nil
			})
		}, "only through non-pointer structs"},
		"unsupported type": {func(r *Sprout) {
			POST(r, "/x", func(context.Context, *struct {
				Meta map[string]string `json:"meta" default:"a"`
			}) (*securityTestResponse, error) {
				return nil, nil
			})
		}, "does not support defaults"},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				recovered := recover()
				if msg := fmt.Sprint(recovered); recovered == nil || !strings.Contains(msg, tc.want) {
					t.Fatalf("panic = %v, want message containing %q", recovered, tc.want)
				}
			}()
			tc.register(New())
		})
	}
}

type enumDocBody struct {
	Mode     string       `json:"mode" validate:"required,oneof=fast 'very slow'"`
	Level    int          `json:"level" validate:"oneof=1 2 3" default:"2"`
	Grant    string       `json:"grant" validate:"eq=client_credentials" default:"client_credentials"`
	Flag     bool         `json:"flag" validate:"eq=true"`
	Tags     []string     `json:"tags" validate:"omitempty,dive,oneof=a b"`
	Size     []int        `json:"size" validate:"eq=2"`
	Wide     string       `json:"wide" validate:"oneof=a b|eq=c"`
	Status   isolatedEnum `json:"status" default:"open"`
	Required string       `json:"required" validate:"required"`
}

type enumDocRequest struct {
	Page int         `query:"page" validate:"required,oneof=1 2" default:"1"`
	Sort []string    `query:"sort" validate:"omitempty,dive,oneof=asc desc" default:"asc"`
	Body enumDocBody `body:""`
}

func TestOpenAPIEnumsAndDefaultsFromTags(t *testing.T) {
	router := NewWithConfig(nil, WithOpenAPISchemaResolver(enumResolver("status")))
	POST(router, "/enum", func(context.Context, *enumDocRequest) (*securityTestResponse, error) { return nil, nil })

	doc := exportDoc(t, router)
	op := doc.Paths.Value("/enum").Post
	params := map[string]*openapi3.Parameter{}
	for _, p := range op.Parameters {
		params[p.Value.Name] = p.Value
	}
	if p := params["page"]; p.Required || !reflect.DeepEqual(p.Schema.Value.Enum, []any{1.0, 2.0}) || p.Schema.Value.Default != 1.0 {
		t.Fatalf("page param = required %v enum %v default %v", p.Required, p.Schema.Value.Enum, p.Schema.Value.Default)
	}
	if s := params["sort"].Schema.Value; !reflect.DeepEqual(s.Items.Value.Enum, []any{"asc", "desc"}) || !reflect.DeepEqual(s.Default, []any{"asc"}) {
		t.Fatalf("sort param = items enum %v default %v", s.Items.Value.Enum, s.Default)
	}

	body := doc.Components.Schemas[schemaComponentName(reflect.TypeOf(enumDocBody{}))].Value
	if !reflect.DeepEqual(body.Required, []string{"mode", "required"}) {
		t.Fatalf("required = %v", body.Required)
	}
	for name, want := range map[string]struct {
		enum []any
		def  any
	}{
		"mode":     {enum: []any{"fast", "very slow"}},
		"level":    {enum: []any{1.0, 2.0, 3.0}, def: 2.0},
		"grant":    {enum: []any{"client_credentials"}, def: "client_credentials"},
		"flag":     {enum: []any{true}},
		"size":     {},
		"wide":     {},
		"required": {},
	} {
		s := body.Properties[name].Value
		if !reflect.DeepEqual(s.Enum, want.enum) || s.Default != want.def {
			t.Fatalf("%s: enum %v default %v, want %v %v", name, s.Enum, s.Default, want.enum, want.def)
		}
	}
	if items := body.Properties["tags"].Value.Items.Value; !reflect.DeepEqual(items.Enum, []any{"a", "b"}) {
		t.Fatalf("tags items enum = %v", items.Enum)
	}
	// A component reference keeps its own schema; the default rides on a wrapper.
	status := body.Properties["status"]
	if status.Ref != "" || len(status.Value.AllOf) != 1 || status.Value.AllOf[0].Ref == "" || status.Value.Default != "open" {
		t.Fatalf("status = ref %q allOf %v default %v", status.Ref, status.Value.AllOf, status.Value.Default)
	}
	assertRefClosure(t, router)
}
