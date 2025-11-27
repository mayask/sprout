package sprout

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// Test shapes for union type
type CreateUserProps struct {
	Name  string `json:"name" validate:"required,min=2"`
	Email string `json:"email" validate:"required,email"`
}

type UpdateUserProps struct {
	UserID string `json:"user_id" validate:"required,uuid4"`
	Name   string `json:"name" validate:"required"`
}

type DeleteUserProps struct {
	UserID string `json:"user_id" validate:"required,uuid4"`
	Reason string `json:"reason" validate:"required"`
}

// Union request DTO
type UserActionRequest struct {
	Action string           `json:"action" validate:"required,oneof=create update delete"`
	Create *CreateUserProps `json:"-" sprout:"union=properties,when=Action:create" validate:"union"`
	Update *UpdateUserProps `json:"-" sprout:"union=properties,when=Action:update" validate:"union"`
	Delete *DeleteUserProps `json:"-" sprout:"union=properties,when=Action:delete" validate:"union"`
}

type UserActionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func TestUnionTypeBasic(t *testing.T) {
	router := New()

	POST(router, "/action", func(ctx context.Context, req *UserActionRequest) (*UserActionResponse, error) {
		switch req.Action {
		case "create":
			if req.Create == nil {
				t.Error("expected Create to be populated")
			}
			return &UserActionResponse{
				Success: true,
				Message: "User created: " + req.Create.Name,
			}, nil
		case "update":
			if req.Update == nil {
				t.Error("expected Update to be populated")
			}
			return &UserActionResponse{
				Success: true,
				Message: "User updated: " + req.Update.Name,
			}, nil
		case "delete":
			if req.Delete == nil {
				t.Error("expected Delete to be populated")
			}
			return &UserActionResponse{
				Success: true,
				Message: "User deleted for reason: " + req.Delete.Reason,
			}, nil
		}
		return nil, nil
	})

	tests := []struct {
		name           string
		body           string
		expectedStatus int
		expectedMsg    string
	}{
		{
			name: "create action",
			body: `{
				"action": "create",
				"properties": {
					"name": "John Doe",
					"email": "john@example.com"
				}
			}`,
			expectedStatus: http.StatusOK,
			expectedMsg:    "User created: John Doe",
		},
		{
			name: "update action",
			body: `{
				"action": "update",
				"properties": {
					"user_id": "550e8400-e29b-41d4-a716-446655440000",
					"name": "Jane Doe"
				}
			}`,
			expectedStatus: http.StatusOK,
			expectedMsg:    "User updated: Jane Doe",
		},
		{
			name: "delete action",
			body: `{
				"action": "delete",
				"properties": {
					"user_id": "550e8400-e29b-41d4-a716-446655440000",
					"reason": "User requested deletion"
				}
			}`,
			expectedStatus: http.StatusOK,
			expectedMsg:    "User deleted for reason: User requested deletion",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/action", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d. Body: %s", tt.expectedStatus, w.Code, w.Body.String())
			}

			if tt.expectedStatus == http.StatusOK {
				var resp UserActionResponse
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}
				if resp.Message != tt.expectedMsg {
					t.Errorf("expected message %q, got %q", tt.expectedMsg, resp.Message)
				}
			}
		})
	}
}

func TestUnionTypeValidationFailure(t *testing.T) {
	router := New()

	POST(router, "/action", func(ctx context.Context, req *UserActionRequest) (*UserActionResponse, error) {
		return &UserActionResponse{Success: true}, nil
	})

	tests := []struct {
		name           string
		body           string
		expectedStatus int
	}{
		{
			name: "invalid action value",
			body: `{
				"action": "invalid",
				"properties": {"name": "test"}
			}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "create missing required email",
			body: `{
				"action": "create",
				"properties": {
					"name": "John"
				}
			}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "create invalid email",
			body: `{
				"action": "create",
				"properties": {
					"name": "John",
					"email": "not-an-email"
				}
			}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "create name too short",
			body: `{
				"action": "create",
				"properties": {
					"name": "J",
					"email": "j@example.com"
				}
			}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "update invalid uuid",
			body: `{
				"action": "update",
				"properties": {
					"user_id": "not-a-uuid",
					"name": "John"
				}
			}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "delete missing reason",
			body: `{
				"action": "delete",
				"properties": {
					"user_id": "550e8400-e29b-41d4-a716-446655440000"
				}
			}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "missing properties entirely",
			body: `{
				"action": "create"
			}`,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/action", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d. Body: %s", tt.expectedStatus, w.Code, w.Body.String())
			}
		})
	}
}

func TestUnionTagParsing(t *testing.T) {
	tests := []struct {
		name     string
		tag      string
		expected *unionFieldInfo
	}{
		{
			name: "valid tag",
			tag:  "union=properties,when=Type:a",
			expected: &unionFieldInfo{
				jsonFieldName:      "properties",
				discriminatorField: "Type",
				discriminatorValue: "a",
			},
		},
		{
			name: "different field names",
			tag:  "union=data,when=Kind:create",
			expected: &unionFieldInfo{
				jsonFieldName:      "data",
				discriminatorField: "Kind",
				discriminatorValue: "create",
			},
		},
		{
			name:     "missing union field",
			tag:      "when=Type:a",
			expected: nil,
		},
		{
			name:     "missing when clause",
			tag:      "union=properties",
			expected: nil,
		},
		{
			name:     "empty tag",
			tag:      "",
			expected: nil,
		},
		{
			name:     "malformed when clause",
			tag:      "union=properties,when=Type",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseUnionTag(tt.tag)

			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %+v", result)
				}
				return
			}

			if result == nil {
				t.Fatalf("expected %+v, got nil", tt.expected)
			}

			if result.jsonFieldName != tt.expected.jsonFieldName {
				t.Errorf("jsonFieldName: expected %q, got %q", tt.expected.jsonFieldName, result.jsonFieldName)
			}
			if result.discriminatorField != tt.expected.discriminatorField {
				t.Errorf("discriminatorField: expected %q, got %q", tt.expected.discriminatorField, result.discriminatorField)
			}
			if result.discriminatorValue != tt.expected.discriminatorValue {
				t.Errorf("discriminatorValue: expected %q, got %q", tt.expected.discriminatorValue, result.discriminatorValue)
			}
		})
	}
}

func TestCollectUnionFields(t *testing.T) {
	type TestStruct struct {
		Type   string  `json:"type"`
		ShapeA *ShapeA `json:"-" sprout:"union=props,when=Type:a"`
		ShapeB *ShapeB `json:"-" sprout:"union=props,when=Type:b"`
		Normal string  `json:"normal"`
	}

	unions := collectUnionFields(reflect.TypeOf(TestStruct{}))

	if len(unions) != 2 {
		t.Fatalf("expected 2 union fields, got %d", len(unions))
	}

	// Check first union field
	if unions[0].jsonFieldName != "props" {
		t.Errorf("first union jsonFieldName: expected 'props', got %q", unions[0].jsonFieldName)
	}
	if unions[0].discriminatorField != "Type" {
		t.Errorf("first union discriminatorField: expected 'Type', got %q", unions[0].discriminatorField)
	}
	if unions[0].discriminatorValue != "a" {
		t.Errorf("first union discriminatorValue: expected 'a', got %q", unions[0].discriminatorValue)
	}

	// Check second union field
	if unions[1].discriminatorValue != "b" {
		t.Errorf("second union discriminatorValue: expected 'b', got %q", unions[1].discriminatorValue)
	}
}

// Test shapes for simpler tests
type ShapeA struct {
	FieldA string `json:"field_a" validate:"required"`
}

type ShapeB struct {
	FieldB int `json:"field_b" validate:"required,min=1"`
}

func TestUnmarshalUnionFields(t *testing.T) {
	type TestRequest struct {
		Type   string  `json:"type"`
		ShapeA *ShapeA `json:"-" sprout:"union=properties,when=Type:a"`
		ShapeB *ShapeB `json:"-" sprout:"union=properties,when=Type:b"`
	}

	tests := []struct {
		name        string
		jsonBody    string
		checkShapeA bool
		checkShapeB bool
	}{
		{
			name:        "type a populates ShapeA",
			jsonBody:    `{"type": "a", "properties": {"field_a": "hello"}}`,
			checkShapeA: true,
			checkShapeB: false,
		},
		{
			name:        "type b populates ShapeB",
			jsonBody:    `{"type": "b", "properties": {"field_b": 42}}`,
			checkShapeA: false,
			checkShapeB: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req TestRequest
			var bodyMap map[string]json.RawMessage

			// First unmarshal to get the type field
			if err := json.Unmarshal([]byte(tt.jsonBody), &req); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			// Also unmarshal to map
			if err := json.Unmarshal([]byte(tt.jsonBody), &bodyMap); err != nil {
				t.Fatalf("failed to unmarshal to map: %v", err)
			}

			// Collect union fields and unmarshal them
			unions := collectUnionFields(reflect.TypeOf(TestRequest{}))
			if err := unmarshalUnionFields(reflect.ValueOf(&req), bodyMap, unions); err != nil {
				t.Fatalf("failed to unmarshal union fields: %v", err)
			}

			if tt.checkShapeA {
				if req.ShapeA == nil {
					t.Error("expected ShapeA to be populated")
				} else if req.ShapeA.FieldA != "hello" {
					t.Errorf("expected ShapeA.FieldA to be 'hello', got %q", req.ShapeA.FieldA)
				}
				if req.ShapeB != nil {
					t.Error("expected ShapeB to be nil")
				}
			}

			if tt.checkShapeB {
				if req.ShapeB == nil {
					t.Error("expected ShapeB to be populated")
				} else if req.ShapeB.FieldB != 42 {
					t.Errorf("expected ShapeB.FieldB to be 42, got %d", req.ShapeB.FieldB)
				}
				if req.ShapeA != nil {
					t.Error("expected ShapeA to be nil")
				}
			}
		})
	}
}

func TestUnionWithNestedValidation(t *testing.T) {
	type Address struct {
		Street string `json:"street" validate:"required"`
		City   string `json:"city" validate:"required"`
	}

	type CreateWithAddress struct {
		Name    string  `json:"name" validate:"required"`
		Address Address `json:"address" validate:"required"`
	}

	type RequestWithNestedUnion struct {
		Action string             `json:"action" validate:"required,oneof=create"`
		Create *CreateWithAddress `json:"-" sprout:"union=properties,when=Action:create" validate:"union"`
	}

	router := New()

	POST(router, "/nested", func(ctx context.Context, req *RequestWithNestedUnion) (*UserActionResponse, error) {
		return &UserActionResponse{
			Success: true,
			Message: "Created in " + req.Create.Address.City,
		}, nil
	})

	t.Run("valid nested object", func(t *testing.T) {
		body := `{
			"action": "create",
			"properties": {
				"name": "John",
				"address": {
					"street": "123 Main St",
					"city": "Springfield"
				}
			}
		}`

		req := httptest.NewRequest(http.MethodPost, "/nested", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d. Body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("missing nested required field", func(t *testing.T) {
		body := `{
			"action": "create",
			"properties": {
				"name": "John",
				"address": {
					"street": "123 Main St"
				}
			}
		}`

		req := httptest.NewRequest(http.MethodPost, "/nested", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d. Body: %s", w.Code, w.Body.String())
		}
	})
}

// Test union types in responses
type ResponseShapeA struct {
	Message string `json:"message"`
	Count   int    `json:"count"`
}

type ResponseShapeB struct {
	Status string `json:"status"`
	Code   int    `json:"code"`
}

type UnionResponse struct {
	Type   string          `json:"type" validate:"required,oneof=a b"`
	ShapeA *ResponseShapeA `json:"-" sprout:"union=data,when=Type:a" validate:"union"`
	ShapeB *ResponseShapeB `json:"-" sprout:"union=data,when=Type:b" validate:"union"`
}

type SimpleRequest struct {
	WantType string `json:"want_type" validate:"required,oneof=a b"`
}

func TestUnionResponseSerialization(t *testing.T) {
	router := New()

	POST(router, "/respond", func(ctx context.Context, req *SimpleRequest) (*UnionResponse, error) {
		switch req.WantType {
		case "a":
			return &UnionResponse{
				Type: "a",
				ShapeA: &ResponseShapeA{
					Message: "Hello from shape A",
					Count:   42,
				},
			}, nil
		case "b":
			return &UnionResponse{
				Type: "b",
				ShapeB: &ResponseShapeB{
					Status: "OK",
					Code:   200,
				},
			}, nil
		}
		return nil, nil
	})

	tests := []struct {
		name         string
		wantType     string
		expectedData map[string]interface{}
	}{
		{
			name:     "response with shape A",
			wantType: "a",
			expectedData: map[string]interface{}{
				"message": "Hello from shape A",
				"count":   float64(42),
			},
		},
		{
			name:     "response with shape B",
			wantType: "b",
			expectedData: map[string]interface{}{
				"status": "OK",
				"code":   float64(200),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"want_type": "` + tt.wantType + `"}`
			req := httptest.NewRequest(http.MethodPost, "/respond", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d. Body: %s", w.Code, w.Body.String())
			}

			var resp map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			// Check type field
			if resp["type"] != tt.wantType {
				t.Errorf("expected type %q, got %q", tt.wantType, resp["type"])
			}

			// Check data field contains the right shape
			data, ok := resp["data"].(map[string]interface{})
			if !ok {
				t.Fatalf("expected 'data' field to be an object, got %T: %v", resp["data"], resp["data"])
			}

			for key, expectedVal := range tt.expectedData {
				if data[key] != expectedVal {
					t.Errorf("expected data[%q] = %v, got %v", key, expectedVal, data[key])
				}
			}
		})
	}
}
