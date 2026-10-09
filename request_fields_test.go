package sprout

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/go-playground/validator/v10"
)

type fieldErrItem struct {
	Name string `json:"name" validate:"required"`
}

type fieldErrBody struct {
	GrantType string         `json:"grant_type" validate:"required,oneof=client_credentials"`
	Items     []fieldErrItem `json:"items" validate:"dive"`
	Status    bindingStatus  `json:"status"`
}

type fieldErrRequest struct {
	ID          string          `path:"id" validate:"uuid"`
	DateFrom    string          `query:"date_from" validate:"omitempty,datetime=2006-01-02"`
	Statuses    []bindingStatus `query:"statuses"`
	Correlation string          `header:"X-Correlation-ID" validate:"required"`
	Body        fieldErrBody    `body:""`
}

type fieldErrFormRequest struct {
	Body struct {
		ClientID string `json:"client_id" validate:"required"`
		TTL      int    `json:"ttl" form:"expires_in"`
	} `body:"" contentType:"application/x-www-form-urlencoded"`
}

type fieldErrImplicitRequest struct {
	Page  int            `query:"page" validate:"max=5"`
	Name  string         `json:"name" validate:"required"`
	Items []fieldErrItem `json:"items" validate:"dive"`
}

type fieldErrIntPath struct {
	ID int `path:"id"`
}

func newFieldErrorRouter(t *testing.T, captured *error) *Sprout {
	t.Helper()
	router := NewWithConfig(&Config{
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			*captured = err
			w.WriteHeader(http.StatusBadRequest)
		},
	})
	router.RegisterTypeValidationFunc(func(v reflect.Value) error {
		if status, ok := v.Interface().(bindingStatus); ok && status == "BAD" {
			return fmt.Errorf("unknown status %q", status)
		}
		return nil
	})
	ok := func(context.Context, *fieldErrRequest) (*securityTestResponse, error) {
		return &securityTestResponse{}, nil
	}
	POST(router, "/explicit/:id", ok)
	POST(router, "/form", func(context.Context, *fieldErrFormRequest) (*securityTestResponse, error) {
		return &securityTestResponse{}, nil
	})
	POST(router, "/implicit", func(context.Context, *fieldErrImplicitRequest) (*securityTestResponse, error) {
		return &securityTestResponse{}, nil
	})
	GET(router, "/int/:id", func(context.Context, *fieldErrIntPath) (*securityTestResponse, error) {
		return &securityTestResponse{}, nil
	})
	return router
}

const fieldErrID = "0190c9d2-7a3e-7b44-9a5e-1f2a3b4c5d6e"

func TestErrorFieldsUseRequestNames(t *testing.T) {
	type want struct {
		location ParameterSource
		field    string
		tag      string
		param    string
	}
	for _, tc := range []struct {
		name, target, contentType, body string
		header                          http.Header
		kind                            ErrorKind
		want                            []want
	}{
		{
			name:   "validator errors on path, query, header, and explicit body",
			target: "/explicit/not-a-uuid?date_from=21-08-2026", contentType: "application/json",
			body: `{"grant_type":"password","items":[{"name":"a"},{}]}`,
			kind: ErrorKindValidation,
			want: []want{
				{ParameterSourcePath, "id", "uuid", ""},
				{ParameterSourceQuery, "date_from", "datetime", "2006-01-02"},
				{ParameterSourceHeader, "X-Correlation-ID", "required", ""},
				{ParameterSourceBody, "grant_type", "oneof", "client_credentials"},
				{ParameterSourceBody, "items[1].name", "required", ""},
			},
		},
		{
			name:   "type validator errors in query and explicit body",
			target: "/explicit/" + fieldErrID + "?statuses=OK&statuses=BAD", contentType: "application/json",
			header: http.Header{"X-Correlation-ID": {"c1"}},
			body:   `{"grant_type":"client_credentials","status":"BAD"}`,
			kind:   ErrorKindValidation,
			want: []want{
				{ParameterSourceQuery, "statuses[1]", "type", ""},
				{ParameterSourceBody, "status", "type", ""},
			},
		},
		{
			name: "JSON decode errors are body-relative", target: "/explicit/" + fieldErrID, contentType: "application/json",
			body: `{"grant_type":"client_credentials","items":[{"name":7}]}`,
			kind: ErrorKindValidation,
			want: []want{{ParameterSourceBody, "items[0].name", "decode", ""}},
		},
		{
			name: "form errors use the form key", target: "/form", contentType: formURLEncodedContentType,
			body: "expires_in=soon",
			kind: ErrorKindValidation,
			want: []want{{ParameterSourceBody, "expires_in", "decode", ""}},
		},
		{
			name: "form validation uses the json name", target: "/form", contentType: formURLEncodedContentType,
			body: "expires_in=5",
			kind: ErrorKindValidation,
			want: []want{{ParameterSourceBody, "client_id", "required", ""}},
		},
		{
			name: "implicit body and query", target: "/implicit?page=9", contentType: "application/json",
			body: `{"items":[{}]}`,
			kind: ErrorKindValidation,
			want: []want{
				{ParameterSourceQuery, "page", "max", "5"},
				{ParameterSourceBody, "name", "required", ""},
				{ParameterSourceBody, "items[0].name", "required", ""},
			},
		},
		{
			name: "path parse error", target: "/int/abc",
			kind: ErrorKindParse,
			want: []want{{ParameterSourcePath, "id", "decode", ""}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var captured error
			router := newFieldErrorRouter(t, &captured)
			method := http.MethodPost
			if strings.HasPrefix(tc.target, "/int/") {
				method = http.MethodGet
			}
			req := httptest.NewRequest(method, tc.target, strings.NewReader(tc.body))
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			for k, values := range tc.header {
				for _, v := range values {
					req.Header.Add(k, v)
				}
			}
			router.ServeHTTP(httptest.NewRecorder(), req)

			var sproutErr *Error
			if !errors.As(captured, &sproutErr) || sproutErr.Kind != tc.kind {
				t.Fatalf("err %v, want kind %s", captured, tc.kind)
			}
			var got []want
			for _, f := range sproutErr.Fields {
				got = append(got, want{f.Location, f.Field, f.Tag, f.Param})
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("fields = %+v\nwant     %+v", got, tc.want)
			}
		})
	}
}

func TestErrorFieldsCarryValueKindAndCause(t *testing.T) {
	var captured error
	router := newFieldErrorRouter(t, &captured)

	req := httptest.NewRequest(http.MethodPost, "/explicit/"+fieldErrID+"?date_from=x", strings.NewReader(`{"grant_type":"client_credentials","status":"BAD"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Correlation-ID", "c1")
	router.ServeHTTP(httptest.NewRecorder(), req)

	var sproutErr *Error
	errors.As(captured, &sproutErr)
	if len(sproutErr.Fields) != 1 {
		t.Fatalf("fields = %+v", sproutErr.Fields)
	}
	if f := sproutErr.Fields[0]; f.Kind != reflect.String || f.Value != "x" || f.Err != nil {
		t.Fatalf("validator field = %+v", f)
	}
	// The legacy error keeps working and now uses request names too.
	var legacy validator.ValidationErrors
	if !errors.As(captured, &legacy) || legacy[0].Field() != "date_from" {
		t.Fatalf("legacy validator error = %v", captured)
	}

	req = httptest.NewRequest(http.MethodPost, "/explicit/"+fieldErrID, strings.NewReader(`{"grant_type":"client_credentials","status":"BAD"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Correlation-ID", "c1")
	router.ServeHTTP(httptest.NewRecorder(), req)
	errors.As(captured, &sproutErr)
	if f := sproutErr.Fields[0]; f.Kind != reflect.String || f.Value != bindingStatus("BAD") || f.Err == nil || !strings.Contains(f.Err.Error(), "unknown status") {
		t.Fatalf("type validator field = %+v", f)
	}
}
