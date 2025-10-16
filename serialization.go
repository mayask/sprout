package sprout

import (
	"reflect"
	"strconv"
	"strings"
)

// extractStatusCode reads the HTTP status code from struct tags.
// Looks for a field with `http:"status=XXX"` tag.
// Returns defaultCode if no status tag is found.
func extractStatusCode(t reflect.Type, defaultCode int) int {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if httpTag := field.Tag.Get("http"); httpTag != "" {
			// Parse "status=404" or "status=404,description=..."
			parts := strings.Split(httpTag, ",")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if strings.HasPrefix(part, "status=") {
					statusStr := strings.TrimPrefix(part, "status=")
					if code, err := strconv.Atoi(statusStr); err == nil {
						return code
					}
				}
			}
		}
	}
	return defaultCode
}

// extractHeaders reads HTTP headers from named fields with `header:` tags.
// Takes a reflect.Value (not Type) to read field values.
// Returns a map of header names to values.
func extractHeaders(v reflect.Value) map[string]string {
	headers := make(map[string]string)

	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fieldValue := v.Field(i)

		// Look for `header:"Header-Name"` tag
		if headerTag := field.Tag.Get("header"); headerTag != "" {
			// Get the string value of the field
			if fieldValue.Kind() == reflect.String {
				value := fieldValue.String()
				if value != "" {
					headers[headerTag] = value
				}
			}
		}
	}

	return headers
}

// shouldExcludeFromJSON checks if a field should be excluded from JSON serialization.
// Fields with path, query, header, or http tags are excluded.
func shouldExcludeFromJSON(field reflect.StructField) bool {
	// Check if field has json:"-" tag explicitly
	if jsonTag := field.Tag.Get("json"); jsonTag == "-" {
		return true
	}

	// Exclude fields with routing/metadata tags
	if field.Tag.Get("path") != "" {
		return true
	}
	if field.Tag.Get("query") != "" {
		return true
	}
	if field.Tag.Get("header") != "" {
		return true
	}
	if field.Tag.Get("http") != "" {
		return true
	}

	return false
}

// toJSONMap converts a struct to a map, excluding top-level fields with routing tags.
// Anonymous embedded structs are flattened to match standard JSON encoding behavior.
// Nested objects are included as-is (routing tags only matter at the top level).
func toJSONMap(v interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	typ := val.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		fieldValue := val.Field(i)

		// Skip fields that should be excluded (only checks top-level tags)
		if shouldExcludeFromJSON(field) {
			continue
		}

		// Handle anonymous embedded structs by flattening their fields
		if field.Anonymous && fieldValue.Kind() == reflect.Struct {
			// Recursively flatten embedded struct fields into result
			// Process embedded struct fields directly without calling Interface()
			// to handle unexported embedded types
			embeddedType := fieldValue.Type()
			for j := 0; j < embeddedType.NumField(); j++ {
				embeddedField := embeddedType.Field(j)
				embeddedFieldValue := fieldValue.Field(j)

				// Skip fields that should be excluded
				if shouldExcludeFromJSON(embeddedField) {
					continue
				}

				// Get JSON field name
				jsonName := embeddedField.Name
				if jsonTag := embeddedField.Tag.Get("json"); jsonTag != "" && jsonTag != "-" {
					parts := strings.Split(jsonTag, ",")
					if parts[0] != "" {
						jsonName = parts[0]
					}
					// Check for omitempty
					if len(parts) > 1 && parts[1] == "omitempty" {
						if embeddedFieldValue.IsZero() {
							continue
						}
					}
				}

				// Add the embedded field to result
				if embeddedFieldValue.CanInterface() {
					result[jsonName] = embeddedFieldValue.Interface()
				}
			}
			continue
		}

		// Get JSON field name from tag, or use struct field name
		jsonName := field.Name
		if jsonTag := field.Tag.Get("json"); jsonTag != "" && jsonTag != "-" {
			// Parse json tag (handle "name,omitempty" format)
			parts := strings.Split(jsonTag, ",")
			if parts[0] != "" {
				jsonName = parts[0]
			}
			// Check for omitempty
			if len(parts) > 1 && parts[1] == "omitempty" {
				// Skip zero values
				if fieldValue.IsZero() {
					continue
				}
			}
		}

		// Include the field value as-is (nested structs handled by json.Encoder)
		result[jsonName] = fieldValue.Interface()
	}

	return result
}
