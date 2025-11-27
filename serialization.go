package sprout

import (
	"reflect"
	"strconv"
	"strings"
)

type jsonTagInfo struct {
	Name      string
	OmitEmpty bool
}

func parseJSONTag(field reflect.StructField) jsonTagInfo {
	info := jsonTagInfo{
		Name: field.Name,
	}

	tag := field.Tag.Get("json")
	if tag == "" {
		return info
	}

	if tag == "-" {
		info.Name = ""
		return info
	}

	parts := strings.Split(tag, ",")
	if parts[0] != "" {
		info.Name = parts[0]
	}

	for _, opt := range parts[1:] {
		switch strings.TrimSpace(opt) {
		case "omitempty":
			info.OmitEmpty = true
		}
	}

	return info
}

func hasSproutOption(field reflect.StructField, option string) bool {
	tag := field.Tag.Get("sprout")
	if tag == "" {
		return false
	}

	for _, part := range strings.Split(tag, ",") {
		if strings.TrimSpace(part) == option {
			return true
		}
	}
	return false
}

func isUnwrapField(field reflect.StructField) bool {
	return hasSproutOption(field, "unwrap")
}

// extractStatusCode reads the HTTP status code from struct tags.
// Looks for a field with `http:"status=XXX"` tag.
// Returns defaultCode if no status tag is found.
func extractStatusCode(t reflect.Type, defaultCode int) int {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return defaultCode
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
		if v.IsNil() {
			return headers
		}
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return headers
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
		if val.IsNil() {
			return result
		}
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return result
	}

	typ := val.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		fieldValue := val.Field(i)

		// Handle anonymous embedded structs by flattening their fields
		// Process these BEFORE exclusion checks because embedded structs may have
		// http tags (for status codes) but we still want to flatten their fields
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

				tagInfo := parseJSONTag(embeddedField)
				if tagInfo.Name == "" || isUnwrapField(embeddedField) {
					continue
				}

				jsonName := tagInfo.Name

				// Add the embedded field to result
				if embeddedFieldValue.CanInterface() {
					result[jsonName] = embeddedFieldValue.Interface()
				}
			}
			continue
		}

		// Skip fields that should be excluded (only checks top-level tags)
		// This is checked AFTER embedded struct handling so that embedded structs
		// with http tags still get flattened
		if shouldExcludeFromJSON(field) {
			continue
		}

		tagInfo := parseJSONTag(field)
		if tagInfo.Name == "" || isUnwrapField(field) {
			continue
		}

		if tagInfo.OmitEmpty && fieldValue.IsZero() {
			continue
		}

		// Include the field value as-is (nested structs handled by json.Encoder)
		result[tagInfo.Name] = fieldValue.Interface()
	}

	// Handle union fields - these have json:"-" but should be serialized if active
	serializeUnionFields(val, typ, result)

	return result
}

func unwrapJSONFieldValue(v reflect.Value) (interface{}, bool) {
	if !v.IsValid() {
		return nil, false
	}

	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil, false
		}
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return nil, false
	}

	var (
		result interface{}
		found  bool
	)

	typ := v.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}
		if shouldExcludeFromJSON(field) {
			continue
		}
		if !isUnwrapField(field) {
			continue
		}
		if found {
			return nil, false // Multiple unwrap fields not supported
		}

		fieldValue := v.Field(i)
		if !fieldValue.IsValid() {
			continue
		}

		if fieldValue.Kind() == reflect.Ptr && fieldValue.IsNil() {
			result = nil
		} else if fieldValue.CanInterface() {
			result = fieldValue.Interface()
		} else if fieldValue.CanAddr() {
			result = fieldValue.Addr().Interface()
		} else {
			return nil, false
		}
		found = true
	}

	if !found {
		return nil, false
	}

	return result, true
}

func unwrapJSONFieldType(t reflect.Type) (reflect.Type, bool) {
	if t == nil {
		return nil, false
	}

	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t == nil || t.Kind() != reflect.Struct {
		return nil, false
	}

	var (
		unwrapType reflect.Type
		found      bool
	)

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue
		}
		if shouldExcludeFromJSON(field) {
			continue
		}
		if !isUnwrapField(field) {
			continue
		}
		if found {
			return nil, false // Multiple unwrap fields not supported
		}
		unwrapType = field.Type
		found = true
	}

	if !found {
		return nil, false
	}

	return unwrapType, true
}
