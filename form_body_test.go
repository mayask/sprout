package sprout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type tokenFormBody struct {
	GrantType     string      `json:"grant_type" validate:"required,eq=client_credentials"`
	ClientID      string      `json:"client_id" validate:"required"`
	ClientSecret  string      `json:"client_secret" form:"secret" validate:"required"`
	Scopes        []string    `json:"scopes"`
	TTL           *int        `json:"ttl"`
	Region        bindingCode `json:"region"` // encoding.TextUnmarshaler
	Ignored       string      `json:"-"`
	Hidden        string      `json:"hidden" form:"-"`
	tokenFormMeta             // embedded fields are flattened
}

type tokenFormMeta struct {
	Audience string `json:"audience"`
}

type tokenRequest struct {
	Body tokenFormBody `body:"" contentType:"application/json, application/x-www-form-urlencoded" validate:"required"`
}

type tokenResponse struct {
	Body tokenFormBody `json:"body"`
	TTL  int           `json:"ttl"`
}

func newTokenRouter(t *testing.T, captured *error) *Sprout {
	t.Helper()
	strict := false
	router := NewWithConfig(&Config{
		StrictErrorTypes: &strict,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			*captured = err
			w.WriteHeader(http.StatusBadRequest)
		},
	})
	POST(router, "/token", func(_ context.Context, req *tokenRequest) (*tokenResponse, error) {
		resp := &tokenResponse{Body: req.Body}
		if req.Body.TTL != nil {
			resp.TTL = *req.Body.TTL
		}
		resp.Body.TTL = nil
		return resp, nil
	})
	return router
}

func postToken(t *testing.T, router *Sprout, contentType, body string) (int, tokenResponse) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var resp tokenResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v (%s)", err, rec.Body.String())
		}
	}
	return rec.Code, resp
}

func TestBodyAcceptsEachDeclaredContentType(t *testing.T) {
	want := tokenFormBody{
		GrantType: "client_credentials", ClientID: "id", ClientSecret: "s3cret",
		Scopes: []string{"payments:view", "wallets:view"}, Region: "GBR",
		tokenFormMeta: tokenFormMeta{Audience: "api"},
	}
	for _, tc := range []struct {
		name, contentType, body string
	}{
		{"json", "application/json",
			`{"grant_type":"client_credentials","client_id":"id","client_secret":"s3cret","scopes":["payments:view","wallets:view"],"ttl":60,"region":"GBR","audience":"api"}`},
		{"form with charset, form tag override, unknown keys ignored", "application/x-www-form-urlencoded; charset=utf-8",
			"grant_type=client_credentials&client_id=id&secret=s3cret&scopes=payments:view&scopes=wallets:view&ttl=60&region=gbr&audience=api&scope=ignored&hidden=x&Ignored=x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var captured error
			status, got := postToken(t, newTokenRouter(t, &captured), tc.contentType, tc.body)
			if status != http.StatusOK {
				t.Fatalf("status %d err %v", status, captured)
			}
			if !reflect.DeepEqual(got.Body, want) || got.TTL != 60 {
				t.Fatalf("got %+v ttl %d, want %+v ttl 60", got.Body, got.TTL, want)
			}
		})
	}
}

func TestFormBodyErrors(t *testing.T) {
	const valid = "grant_type=client_credentials&client_id=id&secret=s"
	for _, tc := range []struct {
		name, contentType, body string
		kind                    ErrorKind
		decodeFields            []string // per-field conversion errors, in order
		mentions                string   // substring of the error message
	}{
		{"unsupported content type", "text/plain", valid, ErrorKindParse, nil, "unsupported request content type"},
		{"conversion errors are per-field validation errors", formURLEncodedContentType, valid + "&ttl=soon&region=toolong", ErrorKindValidation, []string{"ttl", "region"}, ""},
		{"validate tags apply", formURLEncodedContentType, "grant_type=password&client_id=id&secret=s", ErrorKindValidation, nil, "grant_type"},
		{"malformed form", formURLEncodedContentType, "grant_type=%zz", ErrorKindParse, nil, "invalid form body"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var captured error
			status, _ := postToken(t, newTokenRouter(t, &captured), tc.contentType, tc.body)
			var sproutErr *Error
			if status != http.StatusBadRequest || !errors.As(captured, &sproutErr) || sproutErr.Kind != tc.kind {
				t.Fatalf("status %d err %v, want kind %s", status, captured, tc.kind)
			}
			if tc.mentions != "" && !strings.Contains(captured.Error(), tc.mentions) {
				t.Fatalf("error %v does not mention %q", captured, tc.mentions)
			}
			if tc.decodeFields != nil {
				var typeErrs TypeValidationErrors
				errors.As(captured, &typeErrs)
				var fields []string
				for _, e := range typeErrs {
					fields = append(fields, e.Field)
				}
				if !reflect.DeepEqual(fields, tc.decodeFields) {
					t.Fatalf("field errors %v, want %v", fields, tc.decodeFields)
				}
			}
		})
	}
}

func TestOpenAPIListsEveryRequestContentType(t *testing.T) {
	var captured error
	doc := exportDoc(t, newTokenRouter(t, &captured))
	content := doc.Paths.Value("/token").Post.RequestBody.Value.Content
	jsonType, formType := content["application/json"], content[formURLEncodedContentType]
	if len(content) != 2 || jsonType == nil || formType == nil {
		t.Fatalf("request content types = %v", reflect.ValueOf(content).MapKeys())
	}
	if jsonType.Schema.Ref == "" || jsonType.Schema.Ref != formType.Schema.Ref {
		t.Fatalf("schemas differ: %q vs %q", jsonType.Schema.Ref, formType.Schema.Ref)
	}
	assertRefClosure(t, newTokenRouter(t, &captured))
}

type nestedFormBody struct {
	Name  string        `json:"name"`
	Inner tokenFormMeta `json:"inner"`
}

type customDecodedForm struct {
	Inner tokenFormMeta `json:"inner"`
}

func TestFormBodyTypeCheckedAtRegistration(t *testing.T) {
	for name, tc := range map[string]struct {
		fn   func()
		want string
	}{
		"nested struct": {func() {
			POST(New(), "/x", func(context.Context, *struct {
				Body nestedFormBody `body:"" contentType:"application/x-www-form-urlencoded"`
			}) (*tokenResponse, error) {
				return nil, nil
			})
		}, "nestedFormBody.Inner"},
		"map field": {func() {
			POST(New(), "/x", func(context.Context, *struct {
				Body struct {
					Meta map[string]string `json:"meta"`
				} `body:"" contentType:"application/json,application/x-www-form-urlencoded"`
			}) (*tokenResponse, error) {
				return nil, nil
			})
		}, ".Meta has type map[string]string"},
		"multiple response content types": {func() {
			GET(New(), "/x", func(context.Context, *EmptyRequest) (*struct {
				Body *FileBody `body:"" contentType:"text/csv,application/pdf"`
			}, error) {
				return nil, nil
			})
		}, "exactly one contentType"},
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

	// A replaced form decoder owns the mapping, so nested fields are allowed.
	router := New()
	router.RegisterRequestBodyDecoder(formURLEncodedContentType, func(_ context.Context, body io.Reader, _ map[string]string, target any) (func() error, *Error) {
		payload, _ := io.ReadAll(body)
		target.(*customDecodedForm).Inner.Audience = fmt.Sprintf("custom:%s", payload)
		return nil, nil
	})
	POST(router, "/custom", func(_ context.Context, req *struct {
		Body customDecodedForm `body:"" contentType:"application/x-www-form-urlencoded"`
	}) (*customDecodedForm, error) {
		return &req.Body, nil
	})
	req := httptest.NewRequest(http.MethodPost, "/custom", strings.NewReader("a=b"))
	req.Header.Set("Content-Type", formURLEncodedContentType)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "custom:a=b") {
		t.Fatalf("custom decoder: %d %s", rec.Code, rec.Body.String())
	}
}
