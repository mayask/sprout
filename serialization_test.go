package sprout

import (
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
