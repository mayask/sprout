package sprout

import (
	"errors"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
)

// requestFieldName is the name a request struct field has on the wire and in
// validation errors: its query, path, or header parameter name, else its JSON
// name. It returns "" when none is set; validator then uses the Go name.
func requestFieldName(field reflect.StructField) string {
	for _, tagName := range []string{"query", "path", "header", "json"} {
		name := strings.SplitN(field.Tag.Get(tagName), ",", 2)[0]
		if name != "" && name != "-" {
			return name
		}
	}
	return ""
}

// requestField describes a top-level request struct field for error mapping.
type requestField struct {
	location ParameterSource
	// name is the field's first segment in validation namespaces.
	name string
	// explicitBody marks the body:"" field, whose name is stripped from paths.
	explicitBody bool
}

// requestFields maps validation namespaces of one request type to request
// locations and request-relative paths.
type requestFields struct {
	rootName string
	byGoName map[string]requestField
	fields   []requestField
}

func newRequestFields(reqType reflect.Type) *requestFields {
	reqType = derefType(reqType)
	rf := &requestFields{byGoName: map[string]requestField{}}
	if reqType == nil || reqType.Kind() != reflect.Struct {
		return rf
	}
	rf.rootName = reqType.Name()
	for i := range reqType.NumField() {
		field := reqType.Field(i)
		info := requestField{location: ParameterSourceBody, name: requestFieldName(field)}
		if info.name == "" {
			info.name = field.Name
		}
		switch {
		case field.Tag.Get("path") != "":
			info.location = ParameterSourcePath
		case field.Tag.Get("query") != "":
			info.location = ParameterSourceQuery
		case field.Tag.Get("header") != "":
			info.location = ParameterSourceHeader
		case hasBodyTag(field):
			info.explicitBody = true
		}
		rf.byGoName[field.Name] = info
		rf.fields = append(rf.fields, info)
	}
	return rf
}

// relative converts a namespace starting with the field's name into a
// request-relative path.
func (f requestField) relative(namespace string) string {
	if !f.explicitBody {
		return namespace
	}
	rest := strings.TrimPrefix(namespace, f.name)
	return strings.TrimPrefix(rest, ".")
}

// fieldForNamespace finds the top-level field a type-validation namespace
// starts with (longest name wins).
func (rf *requestFields) fieldForNamespace(namespace string) (requestField, bool) {
	var best requestField
	found := false
	for _, f := range rf.fields {
		if namespace != f.name && !strings.HasPrefix(namespace, f.name+".") && !strings.HasPrefix(namespace, f.name+"[") {
			continue
		}
		if !found || len(f.name) > len(best.name) {
			best, found = f, true
		}
	}
	return best, found
}

// requestErrors converts request validation errors (rooted at the request
// struct) into field errors.
func (rf *requestFields) requestErrors(err error) FieldErrors {
	var validationErrs validator.ValidationErrors
	if errors.As(err, &validationErrs) {
		out := make(FieldErrors, 0, len(validationErrs))
		for _, fe := range validationErrs {
			namespace := rf.trimRoot(fe.Namespace())
			structNamespace := rf.trimRoot(fe.StructNamespace())
			goName := structNamespace
			if idx := strings.IndexAny(goName, ".["); idx >= 0 {
				goName = goName[:idx]
			}
			info, ok := rf.byGoName[goName]
			if !ok {
				info = requestField{location: ParameterSourceBody}
			}
			out = append(out, FieldError{
				Location: info.location,
				Field:    info.relative(namespace),
				Tag:      fe.Tag(),
				Param:    fe.Param(),
				Kind:     fe.Kind(),
				Value:    fe.Value(),
			})
		}
		return out
	}

	var typeErrs TypeValidationErrors
	if errors.As(err, &typeErrs) {
		out := make(FieldErrors, 0, len(typeErrs))
		for _, te := range typeErrs {
			info, ok := rf.fieldForNamespace(te.Namespace)
			if !ok {
				info = requestField{location: ParameterSourceBody}
			}
			out = append(out, FieldError{
				Location: info.location,
				Field:    info.relative(te.Namespace),
				Tag:      te.Tag,
				Kind:     valueKind(te.Value),
				Value:    te.Value,
				Err:      te.Err,
			})
		}
		return out
	}
	return nil
}

// trimRoot removes validator's leading struct-name segment.
func (rf *requestFields) trimRoot(namespace string) string {
	return strings.TrimPrefix(namespace, rf.rootName+".")
}

// bodyErrors converts body decode errors, whose namespaces are already
// relative to the body, into field errors.
func bodyErrors(err error) FieldErrors {
	var typeErrs TypeValidationErrors
	if !errors.As(err, &typeErrs) {
		return nil
	}
	out := make(FieldErrors, 0, len(typeErrs))
	for _, te := range typeErrs {
		out = append(out, FieldError{
			Location: ParameterSourceBody,
			Field:    te.Namespace,
			Tag:      te.Tag,
			Value:    te.Value,
			Err:      te.Err,
		})
	}
	return out
}

// parameterError builds the parse error for a path, query, or header value
// that could not be converted to the field's type.
func parameterError(source ParameterSource, name, value string, fieldType reflect.Type, err error) *Error {
	label := string(source) + " parameter"
	if source == ParameterSourceHeader {
		label = "header"
	}
	kindType := derefType(fieldType)
	if kindType.Kind() == reflect.Slice && !isTextUnmarshaler(kindType) {
		kindType = derefType(kindType.Elem())
	}
	return &Error{
		Kind:    ErrorKindParse,
		Message: "invalid " + label + " '" + name + "'",
		Err: &ParseParameterError{
			Parameter: name,
			Source:    source,
			Value:     value,
			Err:       err,
		},
		Fields: FieldErrors{{
			Location: source,
			Field:    name,
			Tag:      "decode",
			Kind:     kindType.Kind(),
			Value:    value,
			Err:      err,
		}},
	}
}

func valueKind(value any) reflect.Kind {
	if value == nil {
		return reflect.Invalid
	}
	return derefType(reflect.TypeOf(value)).Kind()
}
