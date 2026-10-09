package sprout

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"reflect"
	"strings"
)

const formURLEncodedContentType = "application/x-www-form-urlencoded"

// decodeFormRequestBody is the built-in application/x-www-form-urlencoded
// decoder. Keys map to fields by form tag, falling back to the json tag name;
// unknown keys are ignored. Values convert like query parameters (scalars,
// encoding.TextUnmarshaler, slices from repeated keys). Conversion failures
// become per-field validation errors, as with JSON bodies.
func decodeFormRequestBody(
	_ context.Context,
	body io.Reader,
	_ map[string]string,
	target any,
) (func() error, *Error) {
	payload, err := io.ReadAll(body)
	if err != nil {
		return nil, &Error{Kind: ErrorKindParse, Message: "failed to read request body", Err: err}
	}
	values, err := url.ParseQuery(string(payload))
	if err != nil {
		return nil, &Error{Kind: ErrorKindParse, Message: "invalid form body", Err: err}
	}

	targetValue := reflect.ValueOf(target)
	if targetValue.Kind() != reflect.Pointer || targetValue.Elem().Kind() != reflect.Struct {
		return nil, &Error{Kind: ErrorKindParse, Message: fmt.Sprintf("form bodies require a struct target, got %T", target)}
	}

	var errs TypeValidationErrors
	decodeFormFields(values, targetValue.Elem(), &errs)
	if len(errs) > 0 {
		return nil, &Error{Kind: ErrorKindValidation, Message: "request field validation failed", Err: errs}
	}
	return nil, nil
}

func decodeFormFields(values url.Values, structValue reflect.Value, errs *TypeValidationErrors) {
	structType := structValue.Type()
	for i := range structType.NumField() {
		field := structType.Field(i)
		if field.Anonymous && field.Type.Kind() == reflect.Struct && !isTextUnmarshaler(field.Type) {
			decodeFormFields(values, structValue.Field(i), errs)
			continue
		}
		if field.PkgPath != "" {
			continue
		}
		name := formFieldName(field)
		if name == "" {
			continue
		}
		raw, ok := values[name]
		if !ok {
			continue
		}
		if bad, err := setQueryFieldValue(structValue.Field(i), raw, false); err != nil {
			*errs = append(*errs, TypeValidationError{
				Namespace: name,
				Field:     name,
				Tag:       "decode",
				Value:     bad,
				Err:       err,
			})
		}
	}
}

// formFieldName returns the form key for field: the form tag name if set,
// otherwise the json tag name. "-" excludes the field.
func formFieldName(field reflect.StructField) string {
	if tag, ok := field.Tag.Lookup("form"); ok {
		name := strings.SplitN(tag, ",", 2)[0]
		if name == "-" {
			return ""
		}
		if name != "" {
			return name
		}
	}
	return parseJSONTag(field).Name
}

// validateFormBodyType rejects body types the form decoder cannot fill, so
// mistakes surface at route registration instead of as silently empty fields.
// Form data is flat: fields must be scalars, encoding.TextUnmarshaler types,
// or slices of those; embedded structs are flattened.
func validateFormBodyType(t reflect.Type) error {
	t = derefType(t)
	if t.Kind() != reflect.Struct {
		return fmt.Errorf("sprout: %s bodies require a struct, got %s", formURLEncodedContentType, t)
	}
	for i := range t.NumField() {
		field := t.Field(i)
		if field.Anonymous && !isTextUnmarshaler(field.Type) {
			if field.Type.Kind() != reflect.Struct {
				return fmt.Errorf("sprout: %s body %s embeds %s; only non-pointer struct embedding is supported", formURLEncodedContentType, t, field.Type)
			}
			if err := validateFormBodyType(field.Type); err != nil {
				return err
			}
			continue
		}
		if field.PkgPath != "" || formFieldName(field) == "" {
			continue
		}
		if !isFormValueType(field.Type, true) {
			return fmt.Errorf("sprout: %s body field %s.%s has type %s; form values must be scalars, encoding.TextUnmarshaler types, or slices of those", formURLEncodedContentType, t, field.Name, field.Type)
		}
	}
	return nil
}

func isFormValueType(t reflect.Type, allowSlice bool) bool {
	t = derefType(t)
	if isTextUnmarshaler(t) {
		return true
	}
	switch t.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	case reflect.Slice:
		return allowSlice && isFormValueType(t.Elem(), false)
	default:
		return false
	}
}
