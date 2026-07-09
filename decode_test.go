package sprout

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- Test types that simulate real strict domain primitives ---

// strictAmount simulates money.Amount: struct with unexported field,
// strict UnmarshalJSON that validates. Rejects negatives and non-decimal characters.
type strictAmount struct {
	value string
}

func (a *strictAmount) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw == "" {
		return nil
	}
	if raw[0] == '-' {
		return &testError{"amount must be non-negative: " + raw}
	}
	for _, c := range raw {
		if (c < '0' || c > '9') && c != '.' {
			return &testError{"invalid decimal: " + raw}
		}
	}
	a.value = raw
	return nil
}

func (a strictAmount) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.value)
}

// strictID simulates eid.ID: string-based, strict UnmarshalJSON.
type strictID string

func (id *strictID) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw) < 3 {
		return &testError{"invalid id: " + raw}
	}
	*id = strictID(raw)
	return nil
}

// strictCurrency simulates money.Currency: string-based, strict UnmarshalJSON.
type strictCurrency string

func (c *strictCurrency) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw == "" {
		return nil
	}
	if len(raw) != 3 {
		return &testError{"currency must be 3 letters: " + raw}
	}
	*c = strictCurrency(raw)
	return nil
}

// strictPhone simulates core.Phone: string-based, NO UnmarshalJSON.
// Used to test type mismatch errors for types that don't implement json.Unmarshaler.
type strictPhone string

// nestedDTO tests nested struct decoding with strict types.
type nestedDTO struct {
	Payment struct {
		Amount   strictAmount   `json:"amount"`
		Currency strictCurrency `json:"currency"`
	} `json:"payment"`
}

// sliceDTO tests strict types inside slices.
type sliceDTO struct {
	Items []strictAmount `json:"items"`
}

// mixedDTO tests multiple strict types at the same level.
type mixedDTO struct {
	Name     string         `json:"name"`
	Amount   strictAmount   `json:"amount"`
	ID       strictID       `json:"id"`
	Currency strictCurrency `json:"currency"`
	Phone    strictPhone    `json:"phone"`
}

// optionalFields tests pointer and omitempty strict types.
type optionalFields struct {
	Amount   *strictAmount   `json:"amount,omitempty"`
	Currency *strictCurrency `json:"currency,omitempty"`
}

// noopResp is a generic empty response type for test handlers.
type noopResp struct{}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

// --- Test helpers ---

// errorCapturingConfig returns a Config that captures errors for assertions.
func errorCapturingConfig(capturedErr *error) *Config {
	return &Config{
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			*capturedErr = err
			w.WriteHeader(http.StatusBadRequest)
		},
	}
}

// assertSproutError extracts *sprout.Error from the captured error and asserts the kind.
func assertSproutError(t *testing.T, err error, kind ErrorKind) *Error {
	t.Helper()
	var sproutErr *Error
	if !errors.As(err, &sproutErr) {
		t.Fatalf("expected *sprout.Error, got %T: %v", err, err)
	}
	if sproutErr.Kind != kind {
		t.Errorf("expected error kind %s, got %s", kind, sproutErr.Kind)
	}
	return sproutErr
}

// assertTypeValidationField checks that TypeValidationErrors contains an error
// for the given field path.
func assertTypeValidationField(t *testing.T, errs TypeValidationErrors, fieldPath string) {
	t.Helper()
	for _, e := range errs {
		if strings.Contains(e.Namespace, fieldPath) || strings.Contains(e.Field, fieldPath) {
			return
		}
	}
	t.Errorf("expected type validation error for field containing %q, got: %v", fieldPath, errs)
}

// assertTypeValidationFieldAbsent checks that TypeValidationErrors does NOT contain
// an error for the given field path.
func assertTypeValidationFieldAbsent(t *testing.T, errs TypeValidationErrors, fieldPath string) {
	t.Helper()
	for _, e := range errs {
		if strings.Contains(e.Namespace, fieldPath) || strings.Contains(e.Field, fieldPath) {
			t.Errorf("should not report error for valid field %q, got: %v", fieldPath, e)
		}
	}
}

// extractTypeValidationErrors pulls TypeValidationErrors from a captured error chain.
func extractTypeValidationErrors(t *testing.T, err error) TypeValidationErrors {
	t.Helper()
	var typeErrs TypeValidationErrors
	if !errors.As(err, &typeErrs) {
		t.Fatalf("expected TypeValidationErrors in error chain, got %T: %v", err, err)
	}
	return typeErrs
}

func doRequest(router *Sprout, method, path, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(method, path, strings.NewReader(body)))
	return recorder
}

// --- Tests for custom UnmarshalJSON validation errors (the gap) ---

func TestFieldAwareDecode_CustomValidationError_ReportsFieldName(t *testing.T) {
	var capturedErr error
	called := false
	router := NewWithConfig(errorCapturingConfig(&capturedErr))
	POST(router, "/test", func(ctx context.Context, req *mixedDTO) (*noopResp, error) {
		called = true
		return &noopResp{}, nil
	})

	doRequest(router, http.MethodPost, "/test",
		`{"name":"test","amount":"1xut","id":"abc123","currency":"GBP","phone":"+123"}`)

	if called {
		t.Fatalf("handler should not be called for invalid input")
	}
	sproutErr := assertSproutError(t, capturedErr, ErrorKindValidation)
	typeErrs := extractTypeValidationErrors(t, sproutErr.Err)
	assertTypeValidationField(t, typeErrs, "amount")
}

func TestFieldAwareDecode_NegativeAmount_ReportsFieldName(t *testing.T) {
	var capturedErr error
	called := false
	router := NewWithConfig(errorCapturingConfig(&capturedErr))
	POST(router, "/test", func(ctx context.Context, req *mixedDTO) (*noopResp, error) {
		called = true
		return &noopResp{}, nil
	})

	doRequest(router, http.MethodPost, "/test",
		`{"name":"test","amount":"-10.00","id":"abc123","currency":"GBP","phone":"+123"}`)

	if called {
		t.Fatalf("handler should not be called for negative amount")
	}
	sproutErr := assertSproutError(t, capturedErr, ErrorKindValidation)
	typeErrs := extractTypeValidationErrors(t, sproutErr.Err)
	assertTypeValidationField(t, typeErrs, "amount")
}

func TestFieldAwareDecode_MultipleCustomValidationErrors_ReportsAllFields(t *testing.T) {
	var capturedErr error
	called := false
	router := NewWithConfig(errorCapturingConfig(&capturedErr))
	POST(router, "/test", func(ctx context.Context, req *mixedDTO) (*noopResp, error) {
		called = true
		return &noopResp{}, nil
	})

	doRequest(router, http.MethodPost, "/test",
		`{"name":"test","amount":"bad","id":"x","currency":"TOOLONG","phone":"+123"}`)

	if called {
		t.Fatalf("handler should not be called")
	}
	sproutErr := assertSproutError(t, capturedErr, ErrorKindValidation)
	typeErrs := extractTypeValidationErrors(t, sproutErr.Err)
	assertTypeValidationField(t, typeErrs, "amount")
	assertTypeValidationField(t, typeErrs, "id")
	assertTypeValidationField(t, typeErrs, "currency")
}

func TestFieldAwareDecode_TypeMismatchInCustomType_ReportsFieldName(t *testing.T) {
	var capturedErr error
	called := false
	router := NewWithConfig(errorCapturingConfig(&capturedErr))
	POST(router, "/test", func(ctx context.Context, req *mixedDTO) (*noopResp, error) {
		called = true
		return &noopResp{}, nil
	})

	// amount is strictAmount (struct with UnmarshalJSON that internally
	// unmarshals into string). Sending a number causes a type mismatch
	// inside the custom UnmarshalJSON. The fallback decoder catches this
	// and reports it as a field-level TypeValidationError.
	doRequest(router, http.MethodPost, "/test",
		`{"name":"test","amount":123,"id":"abc123","currency":"GBP","phone":"+123"}`)

	if called {
		t.Fatalf("handler should not be called")
	}
	sproutErr := assertSproutError(t, capturedErr, ErrorKindValidation)
	typeErrs := extractTypeValidationErrors(t, sproutErr.Err)
	assertTypeValidationField(t, typeErrs, "amount")
}

// --- Tests for nested struct decoding ---

func TestFieldAwareDecode_NestedStruct_ReportsNestedFieldPath(t *testing.T) {
	var capturedErr error
	called := false
	router := NewWithConfig(errorCapturingConfig(&capturedErr))
	POST(router, "/nested", func(ctx context.Context, req *nestedDTO) (*noopResp, error) {
		called = true
		return &noopResp{}, nil
	})

	doRequest(router, http.MethodPost, "/nested",
		`{"payment":{"amount":"bad","currency":"GBP"}}`)

	if called {
		t.Fatalf("handler should not be called")
	}
	sproutErr := assertSproutError(t, capturedErr, ErrorKindValidation)
	typeErrs := extractTypeValidationErrors(t, sproutErr.Err)
	assertTypeValidationField(t, typeErrs, "payment.amount")
}

// --- Tests for slice decoding ---

func TestFieldAwareDecode_SliceElement_ReportsIndexedFieldPath(t *testing.T) {
	var capturedErr error
	called := false
	router := NewWithConfig(errorCapturingConfig(&capturedErr))
	POST(router, "/slice", func(ctx context.Context, req *sliceDTO) (*noopResp, error) {
		called = true
		return &noopResp{}, nil
	})

	doRequest(router, http.MethodPost, "/slice",
		`{"items":["10.50","bad","30.00"]}`)

	if called {
		t.Fatalf("handler should not be called")
	}
	sproutErr := assertSproutError(t, capturedErr, ErrorKindValidation)
	typeErrs := extractTypeValidationErrors(t, sproutErr.Err)
	// Must report the specific slice index, not just "items"
	assertTypeValidationField(t, typeErrs, "items[1]")
}

// --- Tests for optional/pointer fields ---

func TestFieldAwareDecode_OptionalField_NilDoesNotError(t *testing.T) {
	var capturedErr error
	called := false
	router := NewWithConfig(errorCapturingConfig(&capturedErr))
	POST(router, "/optional", func(ctx context.Context, req *optionalFields) (*noopResp, error) {
		called = true
		return &noopResp{}, nil
	})

	recorder := doRequest(router, http.MethodPost, "/optional", `{}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 for nil optional fields, got %d", recorder.Code)
	}
	if !called {
		t.Fatalf("handler should be called for valid input")
	}
}

func TestFieldAwareDecode_OptionalField_InvalidValue_ReportsField(t *testing.T) {
	var capturedErr error
	called := false
	router := NewWithConfig(errorCapturingConfig(&capturedErr))
	POST(router, "/optional", func(ctx context.Context, req *optionalFields) (*noopResp, error) {
		called = true
		return &noopResp{}, nil
	})

	doRequest(router, http.MethodPost, "/optional", `{"amount":"bad"}`)

	if called {
		t.Fatalf("handler should not be called")
	}
	sproutErr := assertSproutError(t, capturedErr, ErrorKindValidation)
	typeErrs := extractTypeValidationErrors(t, sproutErr.Err)
	assertTypeValidationField(t, typeErrs, "amount")
}

// --- Tests for non-Unmarshaler types (type mismatch now produces field errors) ---

func TestFieldAwareDecode_NonUnmarshalerTypeMismatch_ReportsField(t *testing.T) {
	var capturedErr error
	called := false
	router := NewWithConfig(errorCapturingConfig(&capturedErr))
	POST(router, "/test", func(ctx context.Context, req *mixedDTO) (*noopResp, error) {
		called = true
		return &noopResp{}, nil
	})

	// phone is strictPhone (string type, no UnmarshalJSON)
	// Sending a number where string is expected now produces a field-level
	// TypeValidationError via the fallback decoder, consistent with custom
	// UnmarshalJSON failures.
	doRequest(router, http.MethodPost, "/test",
		`{"name":"test","amount":"10.50","id":"abc123","currency":"GBP","phone":123}`)

	if called {
		t.Fatalf("handler should not be called")
	}
	sproutErr := assertSproutError(t, capturedErr, ErrorKindValidation)
	typeErrs := extractTypeValidationErrors(t, sproutErr.Err)
	assertTypeValidationField(t, typeErrs, "phone")
}

// --- Tests for malformed JSON (should stay as parse error) ---

func TestFieldAwareDecode_MalformedJSON_StaysParseError(t *testing.T) {
	var capturedErr error
	called := false
	router := NewWithConfig(errorCapturingConfig(&capturedErr))
	POST(router, "/test", func(ctx context.Context, req *mixedDTO) (*noopResp, error) {
		called = true
		return &noopResp{}, nil
	})

	doRequest(router, http.MethodPost, "/test",
		`{"name":"test","amount":"10.50"`)

	if called {
		t.Fatalf("handler should not be called")
	}
	// Malformed JSON must stay as ErrorKindParse, NOT become ErrorKindValidation
	assertSproutError(t, capturedErr, ErrorKindParse)
}

// --- Tests for valid input (happy path unaffected) ---

func TestFieldAwareDecode_ValidInput_HappyPathUnaffected(t *testing.T) {
	var capturedErr error
	called := false
	router := NewWithConfig(errorCapturingConfig(&capturedErr))
	POST(router, "/test", func(ctx context.Context, req *mixedDTO) (*noopResp, error) {
		called = true
		return &noopResp{}, nil
	})

	recorder := doRequest(router, http.MethodPost, "/test",
		`{"name":"test","amount":"10.50","id":"abc123","currency":"GBP","phone":"+12345678"}`)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid input, got %d", recorder.Code)
	}
	if !called {
		t.Fatalf("handler should be called for valid input")
	}
	if capturedErr != nil {
		t.Fatalf("expected no error for valid input, got: %v", capturedErr)
	}
}

// --- Test for mixed valid and invalid fields ---

func TestFieldAwareDecode_MixedValidInvalid_ReportsOnlyInvalidFields(t *testing.T) {
	var capturedErr error
	called := false
	router := NewWithConfig(errorCapturingConfig(&capturedErr))
	POST(router, "/test", func(ctx context.Context, req *mixedDTO) (*noopResp, error) {
		called = true
		return &noopResp{}, nil
	})

	doRequest(router, http.MethodPost, "/test",
		`{"name":"test","amount":"bad","id":"abc123","currency":"GBP","phone":"+123"}`)

	if called {
		t.Fatalf("handler should not be called")
	}
	sproutErr := assertSproutError(t, capturedErr, ErrorKindValidation)
	typeErrs := extractTypeValidationErrors(t, sproutErr.Err)
	assertTypeValidationField(t, typeErrs, "amount")
	assertTypeValidationFieldAbsent(t, typeErrs, "id")
	assertTypeValidationFieldAbsent(t, typeErrs, "currency")
}

// --- Test for multiple custom validation errors in same request ---

func TestFieldAwareDecode_MultipleCustomErrors_ReportsBoth(t *testing.T) {
	var capturedErr error
	called := false
	router := NewWithConfig(errorCapturingConfig(&capturedErr))
	POST(router, "/test", func(ctx context.Context, req *mixedDTO) (*noopResp, error) {
		called = true
		return &noopResp{}, nil
	})

	// Both amount and id have custom UnmarshalJSON validation failures
	// (not type mismatches — the JSON shape is correct, values are invalid).
	doRequest(router, http.MethodPost, "/test",
		`{"name":"test","amount":"bad","id":"x","currency":"GBP","phone":"+123"}`)

	if called {
		t.Fatalf("handler should not be called")
	}
	sproutErr := assertSproutError(t, capturedErr, ErrorKindValidation)
	typeErrs := extractTypeValidationErrors(t, sproutErr.Err)
	assertTypeValidationField(t, typeErrs, "amount")
	assertTypeValidationField(t, typeErrs, "id")
}

// --- Test for null on non-pointer field ---

func TestFieldAwareDecode_NullOnNonPointerField_NoError(t *testing.T) {
	var capturedErr error
	called := false
	router := NewWithConfig(errorCapturingConfig(&capturedErr))
	POST(router, "/test", func(ctx context.Context, req *mixedDTO) (*noopResp, error) {
		called = true
		return &noopResp{}, nil
	})

	recorder := doRequest(router, http.MethodPost, "/test",
		`{"name":"test","amount":null,"id":"abc123","currency":"GBP","phone":"+123"}`)

	// null on non-pointer struct field should leave it as zero-value (not an error)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 for null on non-pointer field, got %d", recorder.Code)
	}
	if !called {
		t.Fatalf("handler should be called")
	}
}

// --- Tests for mixed custom and built-in errors in different field orders ---

func TestFieldAwareDecode_CustomErrorThenBuiltinMismatch_BothReported(t *testing.T) {
	var capturedErr error
	called := false
	router := NewWithConfig(errorCapturingConfig(&capturedErr))
	POST(router, "/test", func(ctx context.Context, req *mixedDTO) (*noopResp, error) {
		called = true
		return &noopResp{}, nil
	})

	// Custom UnmarshalJSON error first (amount), then built-in type mismatch (phone).
	// Both should be reported as TypeValidationErrors regardless of field order.
	doRequest(router, http.MethodPost, "/test",
		`{"name":"test","amount":"bad","id":"abc123","currency":"GBP","phone":123}`)

	if called {
		t.Fatalf("handler should not be called")
	}
	sproutErr := assertSproutError(t, capturedErr, ErrorKindValidation)
	typeErrs := extractTypeValidationErrors(t, sproutErr.Err)
	assertTypeValidationField(t, typeErrs, "amount")
	assertTypeValidationField(t, typeErrs, "phone")
}

func TestFieldAwareDecode_BuiltinMismatchThenCustomError_BothReported(t *testing.T) {
	var capturedErr error
	called := false
	router := NewWithConfig(errorCapturingConfig(&capturedErr))
	POST(router, "/test", func(ctx context.Context, req *mixedDTO) (*noopResp, error) {
		called = true
		return &noopResp{}, nil
	})

	// Built-in type mismatch first (phone), then custom UnmarshalJSON error (amount).
	// Both should be reported as TypeValidationErrors regardless of field order.
	doRequest(router, http.MethodPost, "/test",
		`{"name":"test","phone":123,"amount":"bad","id":"abc123","currency":"GBP"}`)

	if called {
		t.Fatalf("handler should not be called")
	}
	sproutErr := assertSproutError(t, capturedErr, ErrorKindValidation)
	typeErrs := extractTypeValidationErrors(t, sproutErr.Err)
	assertTypeValidationField(t, typeErrs, "phone")
	assertTypeValidationField(t, typeErrs, "amount")
}

// --- Test for nested built-in type mismatch reporting specific nested field ---

func TestFieldAwareDecode_NestedBuiltinMismatch_ReportsNestedFieldPath(t *testing.T) {
	var capturedErr error
	called := false
	router := NewWithConfig(errorCapturingConfig(&capturedErr))

	type innerStruct struct {
		Age int `json:"age"`
	}
	type outerDTO struct {
		Person innerStruct `json:"person"`
	}

	POST(router, "/nested-builtin", func(ctx context.Context, req *outerDTO) (*noopResp, error) {
		called = true
		return &noopResp{}, nil
	})

	// age is int, sending a string should report "person.age", not just "person"
	doRequest(router, http.MethodPost, "/nested-builtin",
		`{"person":{"age":"not-a-number"}}`)

	if called {
		t.Fatalf("handler should not be called")
	}
	sproutErr := assertSproutError(t, capturedErr, ErrorKindValidation)
	typeErrs := extractTypeValidationErrors(t, sproutErr.Err)
	assertTypeValidationField(t, typeErrs, "person.age")
}
