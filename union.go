package sprout

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
)

// unionFieldInfo holds parsed information about a union-tagged field
type unionFieldInfo struct {
	fieldIndex         int
	jsonFieldName      string // The JSON field to unmarshal from (e.g., "properties")
	discriminatorField string // The struct field to check (e.g., "Type")
	discriminatorValue string // The expected value (e.g., "a")
}

// parseUnionTag parses a sprout union tag like "union=properties,when=Type:a"
// Returns nil if not a union tag
func parseUnionTag(tag string) *unionFieldInfo {
	if tag == "" {
		return nil
	}

	info := &unionFieldInfo{}
	parts := strings.Split(tag, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)

		if strings.HasPrefix(part, "union=") {
			info.jsonFieldName = strings.TrimPrefix(part, "union=")
		} else if strings.HasPrefix(part, "when=") {
			when := strings.TrimPrefix(part, "when=")
			// Parse "Field:value"
			colonIdx := strings.Index(when, ":")
			if colonIdx > 0 {
				info.discriminatorField = when[:colonIdx]
				info.discriminatorValue = when[colonIdx+1:]
			}
		}
	}

	// Validate we have all required parts
	if info.jsonFieldName == "" || info.discriminatorField == "" || info.discriminatorValue == "" {
		return nil
	}

	return info
}

// collectUnionFields scans a struct type for union-tagged fields
func collectUnionFields(t reflect.Type) []unionFieldInfo {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}

	var unions []unionFieldInfo

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		sproutTag := field.Tag.Get("sprout")

		if info := parseUnionTag(sproutTag); info != nil {
			info.fieldIndex = i
			unions = append(unions, *info)
		}
	}

	return unions
}

// unmarshalUnionFields handles unmarshaling of union-tagged fields from raw JSON
// It reads the discriminator value and unmarshals the appropriate field
func unmarshalUnionFields(reqValue reflect.Value, bodyMap map[string]json.RawMessage, unions []unionFieldInfo) error {
	if reqValue.Kind() == reflect.Ptr {
		reqValue = reqValue.Elem()
	}
	reqType := reqValue.Type()

	for _, union := range unions {
		// Get discriminator field value
		discriminatorFieldIdx := -1
		for i := 0; i < reqType.NumField(); i++ {
			if reqType.Field(i).Name == union.discriminatorField {
				discriminatorFieldIdx = i
				break
			}
		}

		if discriminatorFieldIdx < 0 {
			return fmt.Errorf("union discriminator field '%s' not found", union.discriminatorField)
		}

		discriminatorValue := reqValue.Field(discriminatorFieldIdx)
		if discriminatorValue.Kind() != reflect.String {
			return fmt.Errorf("union discriminator field '%s' must be a string", union.discriminatorField)
		}

		actualValue := discriminatorValue.String()

		// Check if this union variant matches
		if actualValue != union.discriminatorValue {
			continue
		}

		// Get the raw JSON for this union field
		rawJSON, exists := bodyMap[union.jsonFieldName]
		if !exists || len(rawJSON) == 0 {
			continue
		}

		// Get the target field and unmarshal into it
		targetField := reqValue.Field(union.fieldIndex)
		if !targetField.CanSet() {
			return fmt.Errorf("cannot set union field at index %d", union.fieldIndex)
		}

		// Create a new instance if it's a pointer
		if targetField.Kind() == reflect.Ptr {
			targetField.Set(reflect.New(targetField.Type().Elem()))
		}

		// Unmarshal into the field
		var target interface{}
		if targetField.Kind() == reflect.Ptr {
			target = targetField.Interface()
		} else {
			target = targetField.Addr().Interface()
		}

		if err := json.Unmarshal(rawJSON, target); err != nil {
			return fmt.Errorf("failed to unmarshal union field '%s': %w",
				reqType.Field(union.fieldIndex).Name, err)
		}
	}

	return nil
}

// serializeUnionFields handles serialization of union-tagged fields for responses.
// It finds the active union variant and adds it to the result map.
func serializeUnionFields(val reflect.Value, typ reflect.Type, result map[string]interface{}) {
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		sproutTag := field.Tag.Get("sprout")

		info := parseUnionTag(sproutTag)
		if info == nil {
			continue
		}

		// Get discriminator value
		discriminatorField := val.FieldByName(info.discriminatorField)
		if !discriminatorField.IsValid() {
			continue
		}

		actualValue := discriminatorField.String()

		// Check if this is the active variant
		if actualValue != info.discriminatorValue {
			continue
		}

		// This is the active variant - serialize it if not nil
		fieldValue := val.Field(i)
		if fieldValue.Kind() == reflect.Ptr && fieldValue.IsNil() {
			continue
		}

		// Add to result with the JSON field name from the union tag
		if fieldValue.CanInterface() {
			result[info.jsonFieldName] = fieldValue.Interface()
		}
	}
}

// registerUnionValidation registers the "union" validation tag
// This validator ensures that the active union member (based on discriminator) is valid
func registerUnionValidation(v *validator.Validate) {
	// callValidationEvenIfNull=true ensures we validate nil pointers (inactive union variants)
	v.RegisterValidation("union", func(fl validator.FieldLevel) bool {
		field := fl.Field()
		parent := fl.Parent()

		// Handle pointer parent
		if parent.Kind() == reflect.Ptr {
			parent = parent.Elem()
		}

		parentType := parent.Type()
		if parentType.Kind() == reflect.Ptr {
			parentType = parentType.Elem()
		}

		// Get the sprout tag from the struct field using StructFieldName
		structField, ok := parentType.FieldByName(fl.StructFieldName())
		if !ok {
			return true // Can't find field, skip
		}

		sproutTag := structField.Tag.Get("sprout")
		info := parseUnionTag(sproutTag)
		if info == nil {
			return true // Not a union field, skip
		}

		// Get discriminator value from parent
		discriminatorField := parent.FieldByName(info.discriminatorField)
		if !discriminatorField.IsValid() {
			return false // Discriminator not found
		}

		actualValue := discriminatorField.String()

		// If this is not the active variant, it should be nil/zero
		if actualValue != info.discriminatorValue {
			// Not our variant - field should be nil
			if field.Kind() == reflect.Ptr {
				return field.IsNil()
			}
			return field.IsZero()
		}

		// This IS the active variant - field must be non-nil
		if field.Kind() == reflect.Ptr {
			if field.IsNil() {
				return false
			}
			// The struct inside will be validated by the validator automatically
			// due to dive behavior on pointers
		}

		return true
	}, true) // callValidationEvenIfNull=true
}
