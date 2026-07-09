package sprout

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
)

// decodeJSONBody decodes a JSON request body into target. On success it returns
// nil. On failure it returns a *Error with either ErrorKindValidation (when
// field-level errors are available) or ErrorKindParse (syntax errors or
// unidentifiable failures).
//
// The happy path is a single json.Unmarshal call. Only when that fails does
// the field-aware fallback run to produce granular per-field errors.
func decodeJSONBody(body []byte, target any) *Error {
	if err := json.Unmarshal(body, target); err != nil {
		fallbackErr := fieldAwareDecode(body, target, err)
		var typeValidationErrs TypeValidationErrors
		if errors.As(fallbackErr, &typeValidationErrs) && len(typeValidationErrs) > 0 {
			return &Error{
				Kind:    ErrorKindValidation,
				Message: "request field validation failed",
				Err:     typeValidationErrs,
			}
		}
		return &Error{
			Kind:    ErrorKindParse,
			Message: "invalid JSON",
			Err:     fallbackErr,
		}
	}
	return nil
}

// fieldAwareDecode attempts to decode a JSON body into a struct, collecting
// per-field errors for types that implement json.Unmarshaler or for built-in
// type mismatches. It is used as a fallback when json.Unmarshal fails, so the
// happy path (valid input) pays no cost.
//
// The function walks the raw JSON object and the struct's tagged fields in
// parallel. For each field:
//   - If the field type (or its pointer) implements json.Unmarshaler, it decodes
//     just that field's raw JSON value and records any error with the field name.
//   - If the field does not implement json.Unmarshaler, it attempts a standard
//     json.Unmarshal into a zero value of the field type; type mismatches are
//     recorded as field errors.
//   - If the field is a struct, slice, or map, it recurses into the raw JSON.
//
// Fields that decode successfully are set on the target struct so that
// subsequent tag-based validation (validate.Struct) and type validation
// (validateTypedValues) can run on a partially-decoded DTO.
//
// Malformed JSON (syntax errors) cannot be field-mapped and returns the
// original error unchanged.
func fieldAwareDecode(body []byte, target any, originalErr error) error {
	targetValue := reflect.ValueOf(target)
	if targetValue.Kind() != reflect.Pointer {
		return originalErr
	}
	elem := targetValue.Elem()
	if elem.Kind() != reflect.Struct {
		return originalErr
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		// Syntax error or top-level type mismatch — can't field-map.
		return originalErr
	}

	var errs TypeValidationErrors
	decodeStructFields(raw, elem, "", &errs)

	if len(errs) > 0 {
		return errs
	}
	// Fallback couldn't identify specific field errors — return the original
	// parse error so the caller doesn't proceed with a partially-decoded DTO.
	return originalErr
}

// decodeStructFields walks a struct's exported fields and decodes each from
// the corresponding raw JSON entry.
func decodeStructFields(raw map[string]json.RawMessage, structVal reflect.Value, namespace string, errs *TypeValidationErrors) {
	structType := structVal.Type()
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		if field.PkgPath != "" {
			continue // unexported
		}

		tagInfo := parseJSONTag(field)
		if tagInfo.Name == "" {
			continue // excluded via json:"-" or empty
		}

		rawValue, exists := raw[tagInfo.Name]
		if !exists {
			continue
		}

		fieldVal := structVal.Field(i)
		fieldNS := joinValidationNamespace(namespace, tagInfo.Name)

		decodeFieldValue(rawValue, fieldVal, fieldNS, errs)
	}
}

// decodeFieldValue decodes a single field from its raw JSON value.
func decodeFieldValue(raw json.RawMessage, fieldVal reflect.Value, namespace string, errs *TypeValidationErrors) {
	// Skip null values — leave field at zero value.
	if string(raw) == "null" {
		return
	}

	// Handle pointers: allocate if needed, then decode into the element.
	if fieldVal.Kind() == reflect.Pointer {
		if fieldVal.IsNil() {
			fieldVal.Set(reflect.New(fieldVal.Type().Elem()))
		}
		decodeFieldValue(raw, fieldVal.Elem(), namespace, errs)
		return
	}

	// Check if this field's type implements json.Unmarshaler (on pointer receiver).
	if decoder, ok := fieldVal.Addr().Interface().(json.Unmarshaler); ok {
		if err := decoder.UnmarshalJSON(raw); err != nil {
			*errs = append(*errs, TypeValidationError{
				Namespace: namespace,
				Field:     leafValidationField(namespace),
				Tag:       "decode",
				Value:     rawValueForError(raw),
				Err:       err,
			})
		}
		return
	}

	// Non-Unmarshaler types: try a standard decode into this field.
	// If it fails, record a field-level error instead of aborting the whole body.
	if fieldVal.CanAddr() && fieldVal.CanSet() {
		if err := json.Unmarshal(raw, fieldVal.Addr().Interface()); err != nil {
			// For composite kinds (struct, slice, map), the error may be in a
			// nested field. Try recursing into the raw JSON first to produce
			// a more specific field path. Only report the parent-level error
			// if the raw JSON shape doesn't match the composite kind at all.
			if isCompositeKind(fieldVal.Kind()) && rawIsCompositeShape(raw, fieldVal.Kind()) {
				decodeCompositeValue(raw, fieldVal, namespace, errs)
				return
			}

			// Non-composite or shape mismatch: report with field context.
			var typeErr *json.UnmarshalTypeError
			if isUnmarshalTypeError(err, &typeErr) {
				*errs = append(*errs, TypeValidationError{
					Namespace: namespace,
					Field:     leafValidationField(namespace),
					Tag:       "decode",
					Value:     typeErr.Value,
					Err:       fmt.Errorf("expected %s but got %s", friendlyDecodeTypeName(typeErr.Type), typeErr.Value),
				})
				return
			}
			*errs = append(*errs, TypeValidationError{
				Namespace: namespace,
				Field:     leafValidationField(namespace),
				Tag:       "decode",
				Value:     rawValueForError(raw),
				Err:       err,
			})
		}
		return
	}
}

// decodeCompositeValue handles structs, slices, and maps by recursing into raw JSON.
func decodeCompositeValue(raw json.RawMessage, fieldVal reflect.Value, namespace string, errs *TypeValidationErrors) {
	switch fieldVal.Kind() {
	case reflect.Struct:
		var rawMap map[string]json.RawMessage
		if err := json.Unmarshal(raw, &rawMap); err != nil {
			*errs = append(*errs, TypeValidationError{
				Namespace: namespace,
				Field:     leafValidationField(namespace),
				Tag:       "decode",
				Value:     rawValueForError(raw),
				Err:       err,
			})
			return
		}
		decodeStructFields(rawMap, fieldVal, namespace, errs)

	case reflect.Slice:
		var rawSlice []json.RawMessage
		if err := json.Unmarshal(raw, &rawSlice); err != nil {
			*errs = append(*errs, TypeValidationError{
				Namespace: namespace,
				Field:     leafValidationField(namespace),
				Tag:       "decode",
				Value:     rawValueForError(raw),
				Err:       err,
			})
			return
		}
		// Allocate slice of the right size.
		sliceVal := reflect.MakeSlice(fieldVal.Type(), len(rawSlice), len(rawSlice))
		fieldVal.Set(sliceVal)
		for i, rawElem := range rawSlice {
			elemVal := fieldVal.Index(i)
			elemNS := fmt.Sprintf("%s[%d]", namespace, i)
			decodeFieldValue(rawElem, elemVal, elemNS, errs)
		}

	case reflect.Map:
		keyType := fieldVal.Type().Key()
		// Only recurse into maps with string keys (JSON objects have string keys).
		// For non-string key types, fall back to a standard decode error.
		if keyType.Kind() != reflect.String {
			if err := json.Unmarshal(raw, fieldVal.Addr().Interface()); err != nil {
				*errs = append(*errs, TypeValidationError{
					Namespace: namespace,
					Field:     leafValidationField(namespace),
					Tag:       "decode",
					Value:     rawValueForError(raw),
					Err:       err,
				})
			}
			return
		}

		var rawMap map[string]json.RawMessage
		if err := json.Unmarshal(raw, &rawMap); err != nil {
			*errs = append(*errs, TypeValidationError{
				Namespace: namespace,
				Field:     leafValidationField(namespace),
				Tag:       "decode",
				Value:     rawValueForError(raw),
				Err:       err,
			})
			return
		}
		mapVal := reflect.MakeMapWithSize(fieldVal.Type(), len(rawMap))
		fieldVal.Set(mapVal)
		elemType := fieldVal.Type().Elem()
		for key, rawVal := range rawMap {
			keyVal := reflect.ValueOf(key).Convert(keyType)
			elemVal := reflect.New(elemType).Elem()
			elemNS := fmt.Sprintf("%s[%s]", namespace, key)
			decodeFieldValue(rawVal, elemVal, elemNS, errs)
			fieldVal.SetMapIndex(keyVal, elemVal)
		}
	}
}

// isUnmarshalTypeError checks if an error is (or wraps) a *json.UnmarshalTypeError.
func isUnmarshalTypeError(err error, target **json.UnmarshalTypeError) bool {
	return errors.As(err, target)
}

// isCompositeKind returns true for kinds that can contain nested values.
// Array is excluded — fixed-size arrays are rare in DTOs and would need
// separate testing. Slice handles the common variable-length case.
func isCompositeKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Struct, reflect.Slice, reflect.Map:
		return true
	default:
		return false
	}
}

// rawIsCompositeShape checks if the raw JSON value has the right top-level
// shape for the given composite kind: object for struct/map, array for slice.
// This avoids recursing into a composite when the JSON shape is fundamentally
// wrong (e.g., a number where an object was expected).
func rawIsCompositeShape(raw json.RawMessage, kind reflect.Kind) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false
	}
	switch kind {
	case reflect.Struct, reflect.Map:
		return trimmed[0] == '{'
	case reflect.Slice:
		return trimmed[0] == '['
	default:
		return false
	}
}

// friendlyDecodeTypeName converts a reflect.Type to a user-friendly name.
func friendlyDecodeTypeName(t reflect.Type) string {
	if t == nil {
		return "unknown"
	}
	switch t.Kind() {
	case reflect.Struct:
		return "object"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Map:
		return "object"
	case reflect.Ptr:
		return friendlyDecodeTypeName(t.Elem())
	default:
		return t.String()
	}
}

// rawValueForError extracts a short string representation of the raw JSON value
// for error messages.
func rawValueForError(raw json.RawMessage) any {
	s := string(raw)
	if len(s) > 50 {
		s = s[:50] + "..."
	}
	return s
}
