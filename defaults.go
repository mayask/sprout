package sprout

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// fieldDefault is a field's converted default:"..." value and its index path.
type fieldDefault struct {
	index []int
	value reflect.Value
}

// requestDefaults holds a route's defaults, resolved at registration. request
// defaults are relative to the request struct and pre-populated before
// binding, so values present in the query or body overwrite them. body
// defaults are relative to a pointer body's element and applied once the
// body is allocated.
type requestDefaults struct {
	request []fieldDefault
	body    []fieldDefault
}

func applyDefaults(target reflect.Value, defaults []fieldDefault) {
	for _, d := range defaults {
		target.FieldByIndex(d.index).Set(cloneDefault(d.value))
	}
}

// cloneDefault copies pointers and slices so requests never share a default's
// backing memory.
func cloneDefault(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		clone := reflect.New(v.Type().Elem())
		clone.Elem().Set(cloneDefault(v.Elem()))
		return clone
	case reflect.Slice:
		clone := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		reflect.Copy(clone, v)
		return clone
	default:
		return v
	}
}

// convertDefault converts a default tag like a query value: scalars,
// encoding.TextUnmarshaler types, and slices of those, whose defaults are
// comma-separated.
func convertDefault(t reflect.Type, raw string) (reflect.Value, error) {
	if raw == "" {
		return reflect.Value{}, fmt.Errorf("default is empty")
	}
	if !isFormValueType(t, true) {
		return reflect.Value{}, fmt.Errorf("type %s does not support defaults; use scalars, encoding.TextUnmarshaler types, or slices of those", t)
	}
	value := reflect.New(t).Elem()
	if _, err := setQueryFieldValue(value, []string{raw}, true); err != nil {
		return reflect.Value{}, err
	}
	return value, nil
}

// buildRequestDefaults resolves every default tag of a request type. Defaults
// are allowed on query fields and on body fields reachable through
// non-pointer structs; anywhere else (path, header, inside pointers, slices,
// or maps) they panic at registration rather than being silently ignored.
func buildRequestDefaults(reqType reflect.Type, body *requestBodyField) (requestDefaults, error) {
	var defaults requestDefaults
	reqType = derefType(reqType)
	if reqType == nil || reqType.Kind() != reflect.Struct {
		return defaults, nil
	}
	for i := range reqType.NumField() {
		field := reqType.Field(i)
		if field.PkgPath != "" && !field.Anonymous {
			continue
		}
		raw, hasDefault := field.Tag.Lookup("default")
		switch {
		case field.Tag.Get("path") != "" || field.Tag.Get("header") != "":
			if hasDefault {
				return defaults, fmt.Errorf("sprout: default on %s.%s: defaults are supported on query and body fields only", reqType, field.Name)
			}
		case field.Tag.Get("query") != "":
			if hasDefault {
				value, err := convertDefault(field.Type, raw)
				if err != nil {
					return defaults, fmt.Errorf("sprout: invalid default for %s.%s: %w", reqType, field.Name, err)
				}
				defaults.request = append(defaults.request, fieldDefault{index: []int{i}, value: value})
			}
		case body != nil && body.index == i:
			if hasDefault {
				return defaults, fmt.Errorf("sprout: default on body field %s.%s is not supported; set defaults on the body's fields", reqType, field.Name)
			}
			if isStreamBodyType(field.Type) {
				continue
			}
			if field.Type.Kind() == reflect.Pointer {
				if err := collectStructDefaults(field.Type.Elem(), nil, &defaults.body); err != nil {
					return defaults, err
				}
			} else if err := collectStructDefaults(field.Type, []int{i}, &defaults.request); err != nil {
				return defaults, err
			}
		default:
			// Implicit JSON body field.
			if err := collectFieldDefaults(reqType, field, []int{i}, &defaults.request); err != nil {
				return defaults, err
			}
		}
	}
	return defaults, nil
}

func collectStructDefaults(t reflect.Type, prefix []int, out *[]fieldDefault) error {
	if t.Kind() != reflect.Struct || isTextUnmarshaler(t) {
		return nil
	}
	for i := range t.NumField() {
		field := t.Field(i)
		if field.PkgPath != "" && !field.Anonymous {
			continue
		}
		if err := collectFieldDefaults(t, field, appendIndex(prefix, i), out); err != nil {
			return err
		}
	}
	return nil
}

func collectFieldDefaults(owner reflect.Type, field reflect.StructField, index []int, out *[]fieldDefault) error {
	if raw, ok := field.Tag.Lookup("default"); ok {
		value, err := convertDefault(field.Type, raw)
		if err != nil {
			return fmt.Errorf("sprout: invalid default for %s.%s: %w", owner, field.Name, err)
		}
		*out = append(*out, fieldDefault{index: index, value: value})
		return nil
	}
	if field.Type.Kind() == reflect.Struct {
		return collectStructDefaults(field.Type, index, out)
	}
	if path := findNestedDefault(field.Type, map[reflect.Type]bool{}); path != "" {
		return fmt.Errorf("sprout: default on %s inside %s.%s (%s) is not supported; defaults apply only through non-pointer structs", path, owner, field.Name, field.Type)
	}
	return nil
}

// findNestedDefault reports a field carrying a default tag anywhere inside t.
func findNestedDefault(t reflect.Type, seen map[reflect.Type]bool) string {
	for {
		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
			t = t.Elem()
			continue
		}
		break
	}
	if t.Kind() != reflect.Struct || seen[t] {
		return ""
	}
	seen[t] = true
	for i := range t.NumField() {
		field := t.Field(i)
		if _, ok := field.Tag.Lookup("default"); ok {
			return t.String() + "." + field.Name
		}
		if path := findNestedDefault(field.Type, seen); path != "" {
			return path
		}
	}
	return ""
}

func appendIndex(prefix []int, i int) []int {
	index := make([]int, len(prefix)+1)
	copy(index, prefix)
	index[len(prefix)] = i
	return index
}

// openAPIDefault renders a default tag as an OpenAPI value of the field's
// type. Registration has already validated the default.
func openAPIDefault(t reflect.Type, raw string) any {
	t = derefType(t)
	if t.Kind() == reflect.Slice && !isTextUnmarshaler(t) {
		items := []any{}
		for _, item := range strings.Split(raw, ",") {
			if item != "" {
				items = append(items, openAPIScalar(derefType(t.Elem()), item))
			}
		}
		return items
	}
	return openAPIScalar(t, raw)
}

// openAPIScalar converts raw to a JSON-compatible value for kind-based types;
// encoding.TextUnmarshaler and string types stay strings.
func openAPIScalar(t reflect.Type, raw string) any {
	if isTextUnmarshaler(t) {
		return raw
	}
	switch t.Kind() {
	case reflect.Bool:
		if v, err := strconv.ParseBool(raw); err == nil {
			return v
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return v
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if v, err := strconv.ParseUint(raw, 10, 64); err == nil {
			return v
		}
	case reflect.Float32, reflect.Float64:
		if v, err := strconv.ParseFloat(raw, 64); err == nil {
			return v
		}
	}
	return raw
}
