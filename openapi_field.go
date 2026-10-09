package sprout

import (
	"reflect"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// isRequiredField reports whether a field is required in the generated
// document: it has a required validation and no default.
func isRequiredField(field reflect.StructField) bool {
	if _, hasDefault := field.Tag.Lookup("default"); hasDefault {
		return false
	}
	return hasRequiredValidation(field.Tag.Get("validate"))
}

// decorateFieldSchema adds what a struct field's tags say about its value: an
// enum inferred from oneof/eq validation (on the field, or on slice items
// after dive) and its default. Referenced components are wrapped, never
// mutated, since they are shared.
func decorateFieldSchema(field reflect.StructField, ref *openapi3.SchemaRef) *openapi3.SchemaRef {
	fieldTokens, itemTokens := splitValidateTag(field.Tag.Get("validate"))
	t := derefType(field.Type)
	if values := enumFromValidation(fieldTokens, t); values != nil {
		ref = withEnum(ref, values)
	}
	if t.Kind() == reflect.Slice && !isTextUnmarshaler(t) && ref.Ref == "" && ref.Value != nil && ref.Value.Items != nil {
		if values := enumFromValidation(itemTokens, derefType(t.Elem())); values != nil {
			if items := withEnum(ref.Value.Items, values); items != ref.Value.Items {
				schema := *ref.Value
				schema.Items = items
				ref = &openapi3.SchemaRef{Value: &schema}
			}
		}
	}
	if raw, ok := field.Tag.Lookup("default"); ok {
		ref = withDefault(ref, openAPIDefault(field.Type, raw))
	}
	return ref
}

func withEnum(ref *openapi3.SchemaRef, values []any) *openapi3.SchemaRef {
	if ref == nil || ref.Ref != "" || ref.Value == nil {
		return ref // a component (e.g. a resolver enum) keeps its own definition
	}
	schema := *ref.Value
	schema.Enum = values
	return &openapi3.SchemaRef{Value: &schema}
}

func withDefault(ref *openapi3.SchemaRef, value any) *openapi3.SchemaRef {
	if ref.Ref != "" || ref.Value == nil {
		// OpenAPI 3.0 ignores siblings of $ref; carry the default on a wrapper.
		return &openapi3.SchemaRef{Value: &openapi3.Schema{AllOf: openapi3.SchemaRefs{ref}, Default: value}}
	}
	schema := *ref.Value
	schema.Default = value
	return &openapi3.SchemaRef{Value: &schema}
}

// splitValidateTag returns the validate tokens that apply to the field and
// those that apply to its elements (between the first dive and the next
// dive or keys).
func splitValidateTag(tag string) (field, items []string) {
	if tag == "" {
		return nil, nil
	}
	tokens := strings.Split(tag, ",")
	for i, token := range tokens {
		if token != "dive" {
			continue
		}
		field = tokens[:i]
		for _, item := range tokens[i+1:] {
			if item == "dive" || item == "keys" {
				break
			}
			items = append(items, item)
		}
		return field, items
	}
	return tokens, nil
}

// enumFromValidation infers enum values from oneof=a b and eq=x for scalar
// kinds. OR-ed tags (|) are skipped because they widen the allowed set.
func enumFromValidation(tokens []string, t reflect.Type) []any {
	switch t.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
	default:
		return nil
	}
	if isTextUnmarshaler(t) {
		return nil
	}
	for _, token := range tokens {
		if strings.Contains(token, "|") {
			continue
		}
		var raw []string
		switch {
		case strings.HasPrefix(token, "oneof="):
			raw = splitOneOf(strings.TrimPrefix(token, "oneof="))
		case strings.HasPrefix(token, "eq="):
			raw = []string{strings.TrimPrefix(token, "eq=")}
		default:
			continue
		}
		values := make([]any, 0, len(raw))
		for _, item := range raw {
			value, ok := enumValue(t, item)
			if !ok {
				return nil
			}
			values = append(values, value)
		}
		if len(values) > 0 {
			return values
		}
	}
	return nil
}

// splitOneOf splits a oneof parameter like the validator does: by spaces,
// with single-quoted values allowed to contain spaces.
func splitOneOf(param string) []string {
	var values []string
	for param = strings.TrimSpace(param); param != ""; param = strings.TrimSpace(param) {
		if param[0] == '\'' {
			if end := strings.IndexByte(param[1:], '\''); end >= 0 {
				values = append(values, param[1:end+1])
				param = param[end+2:]
				continue
			}
		}
		end := strings.IndexAny(param, " \t")
		if end < 0 {
			values = append(values, param)
			break
		}
		values = append(values, param[:end])
		param = param[end:]
	}
	return values
}

func enumValue(t reflect.Type, raw string) (any, bool) {
	switch t.Kind() {
	case reflect.String:
		return raw, true
	case reflect.Bool:
		v, err := strconv.ParseBool(raw)
		return v, err == nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v, err := strconv.ParseInt(raw, 10, 64)
		return v, err == nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v, err := strconv.ParseUint(raw, 10, 64)
		return v, err == nil
	case reflect.Float32, reflect.Float64:
		v, err := strconv.ParseFloat(raw, 64)
		return v, err == nil
	}
	return nil, false
}
