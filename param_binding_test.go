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
	"time"

	"github.com/getkin/kin-openapi/openapi3"
)

type bindingStatus string

// bindingCode is string-backed but implements encoding.TextUnmarshaler, which
// must take precedence over plain string conversion.
type bindingCode string

func (c *bindingCode) UnmarshalText(text []byte) error {
	if len(text) != 3 {
		return fmt.Errorf("code must be 3 characters, got %q", text)
	}
	*c = bindingCode(strings.ToUpper(string(text)))
	return nil
}

// bindingID is struct-backed, so it only parses through encoding.TextUnmarshaler.
type bindingID struct{ value string }

func (id *bindingID) UnmarshalText(text []byte) error {
	value, ok := strings.CutPrefix(string(text), "id_")
	if !ok {
		return errors.New("missing id_ prefix")
	}
	id.value = value
	return nil
}

type queryArrayRequest struct {
	Tags       []string        `query:"tags"`
	Statuses   []bindingStatus `query:"statuses"`
	Pages      []int           `query:"pages"`
	Optional   *[]string       `query:"optional"`
	Currencies []string        `query:"currencies" explode:"false" validate:"omitempty,max=3,dive,len=3"`
	Page       int             `query:"page"`
}

type queryArrayResponse struct {
	Tags        []string        `json:"tags"`
	Statuses    []bindingStatus `json:"statuses"`
	Pages       []int           `json:"pages"`
	OptionalSet bool            `json:"optional_set"`
	Optional    []string        `json:"optional"`
	Currencies  []string        `json:"currencies"`
	Page        int             `json:"page"`
}

type textParamRequest struct {
	ID    bindingID     `path:"id"`
	Code  bindingCode   `query:"code"`
	Codes []bindingCode `query:"codes" explode:"false"`
	IDs   []bindingID   `query:"ids"`
	Since *time.Time    `query:"since"`
	Trace *bindingID    `header:"X-Trace"`
}

type textParamResponse struct {
	ID    string        `json:"id"`
	Code  bindingCode   `json:"code"`
	Codes []bindingCode `json:"codes"`
	IDs   []string      `json:"ids"`
	Since string        `json:"since"`
	Trace string        `json:"trace"`
}

func newBindingRouter(t *testing.T, captured *error) *Sprout {
	t.Helper()
	strict := false
	router := NewWithConfig(&Config{
		StrictErrorTypes: &strict,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			*captured = err
			w.WriteHeader(http.StatusBadRequest)
		},
	})
	GET(router, "/arrays", func(_ context.Context, req *queryArrayRequest) (*queryArrayResponse, error) {
		resp := &queryArrayResponse{
			Tags: req.Tags, Statuses: req.Statuses, Pages: req.Pages,
			Currencies: req.Currencies, Page: req.Page,
		}
		if req.Optional != nil {
			resp.OptionalSet = true
			resp.Optional = *req.Optional
		}
		return resp, nil
	})
	GET(router, "/text/:id", func(_ context.Context, req *textParamRequest) (*textParamResponse, error) {
		resp := &textParamResponse{ID: req.ID.value, Code: req.Code, Codes: req.Codes}
		for _, id := range req.IDs {
			resp.IDs = append(resp.IDs, id.value)
		}
		if req.Since != nil {
			resp.Since = req.Since.UTC().Format(time.RFC3339)
		}
		if req.Trace != nil {
			resp.Trace = req.Trace.value
		}
		return resp, nil
	})
	return router
}

func serveBinding[T any](t *testing.T, router *Sprout, target string, header http.Header) (int, T) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var body T
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %s: %v (%s)", target, err, rec.Body.String())
		}
	}
	return rec.Code, body
}

func TestQueryArrayBinding(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
		want  queryArrayResponse
	}{
		{"repeated keys keep order", "tags=b&tags=a&tags=b", queryArrayResponse{Tags: []string{"b", "a", "b"}}},
		{"single occurrence", "tags=a", queryArrayResponse{Tags: []string{"a"}}},
		{"commas are literal by default", "tags=a,b", queryArrayResponse{Tags: []string{"a,b"}}},
		{"absent key stays nil", "", queryArrayResponse{}},
		{"empty items skipped", "tags=&tags=a&tags=", queryArrayResponse{Tags: []string{"a"}}},
		{"named element type", "statuses=OPEN&statuses=CLOSED", queryArrayResponse{Statuses: []bindingStatus{"OPEN", "CLOSED"}}},
		{"converted elements", "pages=3&pages=1", queryArrayResponse{Pages: []int{3, 1}}},
		{"pointer to slice set when present", "optional=x", queryArrayResponse{OptionalSet: true, Optional: []string{"x"}}},
		{"pointer to slice nil when only empty values", "optional=", queryArrayResponse{}},
		{"explode=false splits commas", "currencies=GBP,EUR", queryArrayResponse{Currencies: []string{"GBP", "EUR"}}},
		{"explode=false also accepts repeated keys", "currencies=GBP,EUR&currencies=USD", queryArrayResponse{Currencies: []string{"GBP", "EUR", "USD"}}},
		{"explode=false skips empty items", "currencies=,GBP,,EUR,", queryArrayResponse{Currencies: []string{"GBP", "EUR"}}},
		{"scalar keeps first occurrence", "page=2&page=5", queryArrayResponse{Page: 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var captured error
			status, got := serveBinding[queryArrayResponse](t, newBindingRouter(t, &captured), "/arrays?"+tc.query, nil)
			if status != http.StatusOK {
				t.Fatalf("status %d, err %v", status, captured)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestQueryArrayErrors(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		kind        ErrorKind
		badValue    string
		field       string
	}{
		{"element parse error reports the element", "pages=1&pages=x", ErrorKindParse, "x", ""},
		{"default mode does not split commas", "pages=1,2", ErrorKindParse, "1,2", ""},
		{"slice validation", "currencies=GBP,EUR,USD,CHF", ErrorKindValidation, "", "Currencies"},
		{"element validation via dive", "currencies=GBP,EURO", ErrorKindValidation, "", "Currencies[1]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var captured error
			status, _ := serveBinding[queryArrayResponse](t, newBindingRouter(t, &captured), "/arrays?"+tc.query, nil)
			var sproutErr *Error
			if status != http.StatusBadRequest || !errors.As(captured, &sproutErr) || sproutErr.Kind != tc.kind {
				t.Fatalf("status %d err %v, want kind %s", status, captured, tc.kind)
			}
			if tc.badValue != "" {
				var paramErr *ParseParameterError
				if !errors.As(captured, &paramErr) || paramErr.Value != tc.badValue || paramErr.Source != ParameterSourceQuery {
					t.Fatalf("parse error = %+v, want value %q", paramErr, tc.badValue)
				}
			}
			if tc.field != "" && !strings.Contains(captured.Error(), tc.field) {
				t.Fatalf("validation error %v does not mention %s", captured, tc.field)
			}
		})
	}
}

func TestTextUnmarshalerParameters(t *testing.T) {
	var captured error
	router := newBindingRouter(t, &captured)

	status, got := serveBinding[textParamResponse](t, router,
		"/text/id_42?code=gbp&codes=eur,usd&ids=id_a&ids=id_b&since=2026-10-08T12:30:00%2B02:00",
		http.Header{"X-Trace": {"id_t1"}})
	if status != http.StatusOK {
		t.Fatalf("status %d err %v", status, captured)
	}
	want := textParamResponse{
		ID:    "42",
		Code:  "GBP", // UnmarshalText wins over string-kind conversion
		Codes: []bindingCode{"EUR", "USD"},
		IDs:   []string{"a", "b"},
		Since: "2026-10-08T10:30:00Z",
		Trace: "t1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	for _, tc := range []struct {
		target string
		header http.Header
		source ParameterSource
		value  string
	}{
		{"/text/42", nil, ParameterSourcePath, "42"},
		{"/text/id_1?code=toolong", nil, ParameterSourceQuery, "toolong"},
		{"/text/id_1?ids=id_a&ids=b", nil, ParameterSourceQuery, "b"},
		{"/text/id_1?since=yesterday", nil, ParameterSourceQuery, "yesterday"},
		{"/text/id_1", http.Header{"X-Trace": {"t1"}}, ParameterSourceHeader, "t1"},
	} {
		captured = nil
		status, _ := serveBinding[textParamResponse](t, router, tc.target, tc.header)
		var paramErr *ParseParameterError
		if status != http.StatusBadRequest || !errors.As(captured, &paramErr) || paramErr.Source != tc.source || paramErr.Value != tc.value {
			t.Fatalf("%s: status %d err %v, want %s parse error for %q", tc.target, status, captured, tc.source, tc.value)
		}
	}
}

func TestOpenAPIParameterSchemasMatchBinding(t *testing.T) {
	var captured error
	doc := exportDoc(t, newBindingRouter(t, &captured))

	params := map[string]*openapi3.Parameter{}
	for _, path := range []string{"/arrays", "/text/{id}"} {
		for _, p := range doc.Paths.Value(path).Get.Parameters {
			params[p.Value.Name] = p.Value
		}
	}

	type want struct {
		typ, format, itemsType string
		explodeFalse           bool
	}
	for name, w := range map[string]want{
		"tags":       {typ: "array", itemsType: "string"},
		"pages":      {typ: "array", itemsType: "integer"},
		"currencies": {typ: "array", itemsType: "string", explodeFalse: true},
		"page":       {typ: "integer"},
		"id":         {typ: "string"},
		"code":       {typ: "string"},
		"codes":      {typ: "array", itemsType: "string", explodeFalse: true},
		"ids":        {typ: "array", itemsType: "string"},
		"since":      {typ: "string", format: "date-time"},
		"X-Trace":    {typ: "string"},
	} {
		p := params[name]
		if p == nil {
			t.Fatalf("missing parameter %s", name)
		}
		s := p.Schema.Value
		if !s.Type.Is(w.typ) || s.Format != w.format {
			t.Fatalf("%s: type %v format %q, want %s %q", name, s.Type, s.Format, w.typ, w.format)
		}
		if w.itemsType != "" && (s.Items == nil || !s.Items.Value.Type.Is(w.itemsType)) {
			t.Fatalf("%s: items %+v, want %s", name, s.Items, w.itemsType)
		}
		explodeFalse := p.Explode != nil && !*p.Explode
		if explodeFalse != w.explodeFalse || (w.explodeFalse && p.Style != openapi3.SerializationForm) {
			t.Fatalf("%s: style %q explode %v, want explode=false %v", name, p.Style, p.Explode, w.explodeFalse)
		}
	}
	if _, ok := doc.Components.Schemas[schemaComponentName(reflect.TypeOf(bindingID{}))]; ok {
		t.Fatal("TextUnmarshaler parameter type should not become an object component")
	}
}
