package sprout

import (
	"reflect"
	"testing"
)

// Test types for embedded struct serialization
type testBaseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type testEmbeddedError struct {
	testBaseError        // Anonymous embedding - should be flattened
	Resource      string `json:"resource,omitempty"`
}

type testNestedError struct {
	Base     testBaseError `json:"base"` // Named field - should be nested
	Resource string        `json:"resource,omitempty"`
}

type testErrorWithHTTPTag struct {
	testBaseError `http:"status=404"` // Anonymous with http tag - should still flatten
	Resource      string              `json:"resource,omitempty"`
}

func TestToJSONMap_EmbeddedStructFlattening(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected map[string]interface{}
	}{
		{
			name: "anonymous embedded struct is flattened",
			input: testEmbeddedError{
				testBaseError: testBaseError{
					Code:    "NOT_FOUND",
					Message: "Resource not found",
				},
				Resource: "user",
			},
			expected: map[string]interface{}{
				"code":     "NOT_FOUND",
				"message":  "Resource not found",
				"resource": "user",
			},
		},
		{
			name: "named embedded struct is nested",
			input: testNestedError{
				Base: testBaseError{
					Code:    "NOT_FOUND",
					Message: "Resource not found",
				},
				Resource: "user",
			},
			expected: map[string]interface{}{
				"base": testBaseError{
					Code:    "NOT_FOUND",
					Message: "Resource not found",
				},
				"resource": "user",
			},
		},
		{
			name: "omitempty on embedded struct fields is respected",
			input: testEmbeddedError{
				testBaseError: testBaseError{
					Code:    "NOT_FOUND",
					Message: "Resource not found",
				},
				// Resource omitted (empty string with omitempty tag)
			},
			expected: map[string]interface{}{
				"code":    "NOT_FOUND",
				"message": "Resource not found",
			},
		},
		{
			name: "embedded struct with http tag is still flattened",
			input: testErrorWithHTTPTag{
				testBaseError: testBaseError{
					Code:    "NOT_FOUND",
					Message: "Resource not found",
				},
				Resource: "user",
			},
			expected: map[string]interface{}{
				"code":     "NOT_FOUND",
				"message":  "Resource not found",
				"resource": "user",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toJSONMap(tt.input)

			// Check all expected keys are present with correct values
			for key, expectedValue := range tt.expected {
				actualValue, exists := result[key]
				if !exists {
					t.Errorf("expected key %q not found in result", key)
					continue
				}

				// For testBaseError comparison, we need to compare fields
				if baseErr, ok := expectedValue.(testBaseError); ok {
					actualBaseErr, ok := actualValue.(testBaseError)
					if !ok {
						t.Errorf("expected testBaseError for key %q, got %T", key, actualValue)
						continue
					}
					if actualBaseErr.Code != baseErr.Code || actualBaseErr.Message != baseErr.Message {
						t.Errorf("key %q: expected %+v, got %+v", key, baseErr, actualBaseErr)
					}
				} else if actualValue != expectedValue {
					t.Errorf("key %q: expected %v, got %v", key, expectedValue, actualValue)
				}
			}

			// Check no unexpected keys are present
			for key := range result {
				if _, expected := tt.expected[key]; !expected {
					t.Errorf("unexpected key %q in result", key)
				}
			}
		})
	}
}

func TestParseJSONTag(t *testing.T) {
	type tags struct {
		Default string
		Named   string `json:"named"`
		Omit    string `json:"omit,omitempty"`
		Unwrap  []int  `json:"values" sprout:"unwrap"`
		Ignore  string `json:"-"`
	}

	typ := reflect.TypeOf(tags{})

	cases := []struct {
		field string
		name  string
		omit  bool
	}{
		{"Default", "Default", false},
		{"Named", "named", false},
		{"Omit", "omit", true},
		{"Unwrap", "values", false},
		{"Ignore", "", false},
	}

	for _, tt := range cases {
		field, ok := typ.FieldByName(tt.field)
		if !ok {
			t.Fatalf("field %s not found", tt.field)
		}

		info := parseJSONTag(field)
		if info.Name != tt.name {
			t.Errorf("%s: expected name %q, got %q", tt.field, tt.name, info.Name)
		}
		if info.OmitEmpty != tt.omit {
			t.Errorf("%s: expected omitEmpty=%v, got %v", tt.field, tt.omit, info.OmitEmpty)
		}
	}
}

func TestIsUnwrapField(t *testing.T) {
	type embedded struct {
		Plain   string `json:"plain"`
		Wrapped []int  `json:"values" sprout:"unwrap"`
	}

	typ := reflect.TypeOf(embedded{})

	plain, _ := typ.FieldByName("Plain")
	if isUnwrapField(plain) {
		t.Fatalf("expected Plain not to be unwrap field")
	}

	wrapped, _ := typ.FieldByName("Wrapped")
	if !isUnwrapField(wrapped) {
		t.Fatalf("expected Wrapped to be unwrap field")
	}
}

func TestToJSONMapSkipsUnwrapField(t *testing.T) {
	type inner struct {
		Value string `json:"value"`
	}

	type outer struct {
		Metadata string  `json:"meta"`
		Data     []inner `json:"data" sprout:"unwrap"`
	}

	payload := outer{
		Metadata: "m1",
		Data: []inner{
			{Value: "first"},
			{Value: "second"},
		},
	}

	result := toJSONMap(payload)

	if len(result) != 1 {
		t.Fatalf("expected 1 key in result, got %d", len(result))
	}

	if got, ok := result["meta"]; !ok || got != "m1" {
		t.Fatalf("expected meta key with value %q, got %v (present=%v)", "m1", got, ok)
	}

	if _, exists := result["data"]; exists {
		t.Fatalf("expected unwrap field to be omitted from JSON map")
	}
}

func TestUnwrapJSONFieldValue(t *testing.T) {
	type inner struct {
		ID int `json:"id"`
	}

	type wrapped struct {
		Items []inner `json:"items" sprout:"unwrap"`
	}

	type multi struct {
		Items []inner `json:"items" sprout:"unwrap"`
		More  []inner `json:"more" sprout:"unwrap"`
	}

	payload := &wrapped{
		Items: []inner{{ID: 1}, {ID: 2}},
	}

	value, ok := unwrapJSONFieldValue(reflect.ValueOf(payload))
	if !ok {
		t.Fatalf("expected unwrap to succeed")
	}

	items, ok := value.([]inner)
	if !ok {
		t.Fatalf("expected unwrap value to be []inner, got %T", value)
	}
	if len(items) != 2 || items[0].ID != 1 || items[1].ID != 2 {
		t.Fatalf("unexpected unwrap result: %+v", items)
	}

	nilVal, ok := unwrapJSONFieldValue(reflect.ValueOf(&wrapped{}))
	if !ok {
		t.Fatalf("expected unwrap to succeed for zero value slice")
	}
	nilSlice, ok := nilVal.([]inner)
	if !ok {
		t.Fatalf("expected zero value unwrap to be []inner, got %T", nilVal)
	}
	if nilSlice != nil {
		t.Fatalf("expected zero value slice to unwrap as nil, got %#v", nilSlice)
	}

	if _, ok := unwrapJSONFieldValue(reflect.ValueOf(&multi{})); ok {
		t.Fatalf("expected unwrap to fail when multiple unwrap fields exist")
	}
}
