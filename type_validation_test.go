package sprout

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type typeValidationStatus string

const (
	typeValidationStatusActive   typeValidationStatus = "active"
	typeValidationStatusInactive typeValidationStatus = "inactive"
)

func registerTypeValidationStatus(router *Sprout) {
	router.RegisterTypeValidationFunc(func(value reflect.Value) error {
		if value.Type() != reflect.TypeOf(typeValidationStatus("")) {
			return nil
		}
		if value.String() == "" {
			return nil
		}
		switch typeValidationStatus(value.String()) {
		case typeValidationStatusActive, typeValidationStatusInactive:
			return nil
		default:
			return fmt.Errorf("must be a known status")
		}
	})
}

func TestTypeValidationFuncValidatesRequestWithoutValidationTag(t *testing.T) {
	router := New()
	registerTypeValidationStatus(router)

	type request struct {
		Status   typeValidationStatus   `json:"status" validate:"required"`
		Optional typeValidationStatus   `json:"optional,omitempty"`
		Items    []typeValidationStatus `json:"items"`
	}
	type response struct {
		Status typeValidationStatus `json:"status"`
	}

	called := false
	POST(router, "/statuses", func(ctx context.Context, req *request) (*response, error) {
		called = true
		return &response{Status: req.Status}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/statuses", strings.NewReader(`{"status":"active","items":["inactive"]}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", recorder.Code, recorder.Body.String())
	}
	if !called {
		t.Fatalf("expected handler to be called")
	}

	called = false
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/statuses", strings.NewReader(`{"status":"bogus"}`)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d with body %s", recorder.Code, recorder.Body.String())
	}
	if called {
		t.Fatalf("expected handler not to be called for invalid enum")
	}
}

func TestTypeValidationFuncIsSharedWithMountedRouters(t *testing.T) {
	router := New()
	registerTypeValidationStatus(router)
	mounted := router.Mount("/api", nil)

	type request struct {
		Status typeValidationStatus `query:"status" validate:"required"`
	}
	type response struct {
		Status typeValidationStatus `json:"status"`
	}

	called := false
	GET(mounted, "/statuses", func(ctx context.Context, req *request) (*response, error) {
		called = true
		return &response{Status: req.Status}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/statuses?status=bogus", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d with body %s", recorder.Code, recorder.Body.String())
	}
	if called {
		t.Fatalf("expected mounted handler not to be called for invalid enum")
	}
}

func TestTypeValidationFuncValidatesResponseWithoutValidationTag(t *testing.T) {
	router := New()
	registerTypeValidationStatus(router)

	type request struct{}
	type response struct {
		Status typeValidationStatus `json:"status"`
	}

	GET(router, "/statuses", func(ctx context.Context, req *request) (*response, error) {
		return &response{Status: typeValidationStatus("bogus")}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/statuses", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d with body %s", recorder.Code, recorder.Body.String())
	}
}

type exampleAmount struct {
	value string
}

func (a *exampleAmount) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	a.value = raw
	return nil
}

type exampleCurrency string

type exampleFXRate struct {
	value string
}

func (r *exampleFXRate) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.value = raw
	return nil
}

type exampleCountry string

type exampleJurisdiction struct {
	kind  string
	value string
}

func (j *exampleJurisdiction) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if strings.HasPrefix(raw, "SPECIAL:") {
		j.kind = "special"
		j.value = strings.TrimPrefix(raw, "SPECIAL:")
		return nil
	}
	j.kind = "country"
	j.value = raw
	return nil
}

func registerExampleCustomTypeValidators(router *Sprout) {
	router.RegisterTypeValidationFunc(func(value reflect.Value) error {
		switch value.Type() {
		case reflect.TypeOf(exampleAmount{}):
			amount := value.Interface().(exampleAmount)
			if amount.value == "" {
				return nil
			}
			parsed, err := strconv.ParseFloat(amount.value, 64)
			if err != nil || parsed < 0 {
				return fmt.Errorf("amount must be non-negative")
			}
		case reflect.TypeOf(exampleCurrency("")):
			currency := value.String()
			if currency == "" {
				return nil
			}
			switch currency {
			case "GBP", "EUR", "USD":
				return nil
			default:
				return fmt.Errorf("currency must be supported")
			}
		case reflect.TypeOf(exampleFXRate{}):
			rate := value.Interface().(exampleFXRate)
			if rate.value == "" {
				return nil
			}
			parsed, err := strconv.ParseFloat(rate.value, 64)
			if err != nil || parsed <= 0 {
				return fmt.Errorf("fx rate must be positive")
			}
		case reflect.TypeOf(exampleCountry("")):
			country := value.String()
			if country == "" {
				return nil
			}
			switch country {
			case "GB", "NL", "US":
				return nil
			default:
				return fmt.Errorf("country must be supported")
			}
		case reflect.TypeOf(exampleJurisdiction{}):
			jurisdiction := value.Interface().(exampleJurisdiction)
			if jurisdiction.value == "" {
				return nil
			}
			switch jurisdiction.kind {
			case "country":
				switch jurisdiction.value {
				case "GB", "NL", "US":
					return nil
				}
			case "special":
				switch jurisdiction.value {
				case "KOSOVO", "DARFUR":
					return nil
				}
			}
			return fmt.Errorf("jurisdiction must be supported")
		}
		return nil
	})
}

func TestTypeValidationFuncHandlesCommonCustomTypeShapes(t *testing.T) {
	router := New()
	registerExampleCustomTypeValidators(router)

	type request struct {
		Amount        exampleAmount                  `json:"amount"`
		Currency      exampleCurrency                `json:"currency"`
		FXRate        *exampleFXRate                 `json:"fx_rate"`
		Countries     []exampleCountry               `json:"countries"`
		Jurisdictions map[string]exampleJurisdiction `json:"jurisdictions"`
	}
	type response struct {
		Currency exampleCurrency `json:"currency"`
	}

	called := false
	POST(router, "/custom-types", func(ctx context.Context, req *request) (*response, error) {
		called = true
		return &response{Currency: req.Currency}, nil
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/custom-types", strings.NewReader(`{
		"amount":"10.50",
		"currency":"GBP",
		"fx_rate":"1.25",
		"countries":["GB","NL"],
		"jurisdictions":{"registered":"GB","special":"SPECIAL:KOSOVO"}
	}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", recorder.Code, recorder.Body.String())
	}
	if !called {
		t.Fatalf("expected handler to be called")
	}

	called = false
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/custom-types", strings.NewReader(`{
		"amount":"10.50",
		"currency":"GBP",
		"fx_rate":"0",
		"countries":["GB","ZZ"],
		"jurisdictions":{"unknown":"SPECIAL:UNKNOWN"}
	}`)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d with body %s", recorder.Code, recorder.Body.String())
	}
	if called {
		t.Fatalf("expected handler not to be called for invalid custom types")
	}
}
