package sprout

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
)

// TypeValidationFunc validates a parsed request or response value based on its
// concrete Go type. It is invoked for every exported value in the DTO tree.
// Return nil for types the function does not handle.
type TypeValidationFunc func(reflect.Value) error

// TypeValidationError describes a validation failure reported by a
// TypeValidationFunc.
type TypeValidationError struct {
	Namespace string
	Field     string
	Tag       string
	Value     any
	Err       error
}

func (e TypeValidationError) Error() string {
	field := e.Field
	if field == "" {
		field = e.Namespace
	}
	if field == "" {
		field = "value"
	}
	if e.Err == nil {
		return fmt.Sprintf("%s failed type validation", field)
	}
	return fmt.Sprintf("%s: %s", field, e.Err.Error())
}

// TypeValidationErrors collects all failures reported by registered type
// validators.
type TypeValidationErrors []TypeValidationError

func (e TypeValidationErrors) Error() string {
	switch len(e) {
	case 0:
		return "type validation failed"
	case 1:
		return e[0].Error()
	default:
		return fmt.Sprintf("%d type validation errors", len(e))
	}
}

type typeValidationRegistry struct {
	mu    sync.RWMutex
	funcs []TypeValidationFunc
}

func newTypeValidationRegistry() *typeValidationRegistry {
	return &typeValidationRegistry{}
}

func (r *typeValidationRegistry) register(fn TypeValidationFunc) {
	if fn == nil {
		return
	}
	r.mu.Lock()
	r.funcs = append(r.funcs, fn)
	r.mu.Unlock()
}

func (r *typeValidationRegistry) validate(value any) error {
	if r == nil {
		return nil
	}

	r.mu.RLock()
	funcs := append([]TypeValidationFunc(nil), r.funcs...)
	r.mu.RUnlock()
	if len(funcs) == 0 {
		return nil
	}

	var errs TypeValidationErrors
	walkTypeValidationValue(reflect.ValueOf(value), "", funcs, &errs)
	if len(errs) > 0 {
		return errs
	}
	return nil
}

func walkTypeValidationValue(value reflect.Value, namespace string, funcs []TypeValidationFunc, errs *TypeValidationErrors) {
	if !value.IsValid() {
		return
	}

	switch value.Kind() {
	case reflect.Interface, reflect.Pointer:
		if value.IsNil() {
			return
		}
		walkTypeValidationValue(value.Elem(), namespace, funcs, errs)
		return
	}

	if value.CanInterface() {
		for _, fn := range funcs {
			if err := fn(value); err != nil {
				*errs = append(*errs, TypeValidationError{
					Namespace: namespace,
					Field:     leafValidationField(namespace),
					Tag:       "type",
					Value:     value.Interface(),
					Err:       err,
				})
			}
		}
	}

	switch value.Kind() {
	case reflect.Struct:
		valueType := value.Type()
		for i := 0; i < value.NumField(); i++ {
			field := valueType.Field(i)
			if field.PkgPath != "" {
				continue
			}
			walkTypeValidationValue(value.Field(i), joinValidationNamespace(namespace, validationFieldName(field)), funcs, errs)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			walkTypeValidationValue(value.Index(i), fmt.Sprintf("%s[%d]", namespace, i), funcs, errs)
		}
	case reflect.Map:
		iter := value.MapRange()
		for iter.Next() {
			walkTypeValidationValue(iter.Value(), fmt.Sprintf("%s[%v]", namespace, iter.Key().Interface()), funcs, errs)
		}
	}
}

func validationFieldName(field reflect.StructField) string {
	if name := requestFieldName(field); name != "" {
		return name
	}
	return field.Name
}

func joinValidationNamespace(parent string, child string) string {
	if child == "" {
		return parent
	}
	if parent == "" {
		return child
	}
	return parent + "." + child
}

func leafValidationField(namespace string) string {
	if namespace == "" {
		return ""
	}
	idx := strings.LastIndex(namespace, ".")
	if idx == -1 {
		return namespace
	}
	return namespace[idx+1:]
}

// RegisterTypeValidationFunc registers a type-aware validation function that is
// run for parsed request and response DTOs in addition to tag-based validation.
func (s *Sprout) RegisterTypeValidationFunc(fn TypeValidationFunc) {
	if s == nil || s.typeValidators == nil {
		return
	}
	s.typeValidators.register(fn)
}

func (s *Sprout) validateTypedValues(value any) error {
	if s == nil || s.typeValidators == nil {
		return nil
	}
	return s.typeValidators.validate(value)
}
