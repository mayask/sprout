package sprout

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/julienschmidt/httprouter"
	"gopkg.in/yaml.v3"
)

// typeOf returns the non-pointer reflect.Type of the generic parameter.
func typeOf[T any]() reflect.Type {
	var zero *T
	return reflect.TypeOf(zero).Elem()
}

type openAPIDocument struct {
	mu        sync.RWMutex
	doc       *openapi3.T
	typeNames map[reflect.Type]string
}

func newOpenAPIDocument() *openAPIDocument {
	components := openapi3.NewComponents()
	components.Schemas = openapi3.Schemas{}

	doc := &openapi3.T{
		OpenAPI: "3.0.3",
		Info: &openapi3.Info{
			Title:   "Sprout API",
			Version: "1.0.0",
		},
		Paths:      openapi3.NewPaths(),
		Components: &components,
	}

	return &openAPIDocument{
		doc:       doc,
		typeNames: make(map[reflect.Type]string),
	}
}

func (d *openAPIDocument) RegisterRoute(method, fullPath string, reqType, respType reflect.Type, expectedErrors []reflect.Type) {
	if d == nil {
		return
	}

	normalizedPath := toOpenAPIPath(fullPath)

	d.mu.Lock()
	defer d.mu.Unlock()

	parameters, requestBody := d.buildRequestArtifactsLocked(reqType)
	successStatus := extractStatusCode(respType, http.StatusOK)
	successSchema := d.schemaRefLocked(respType)

	responses := openapi3.NewResponses()

	successResponse := openapi3.NewResponse().WithDescription("Successful response")
	successResponse.Content = openapi3.Content{
		"application/json": &openapi3.MediaType{
			Schema: successSchema,
		},
	}
	responses.Set(strconv.Itoa(successStatus), &openapi3.ResponseRef{Value: successResponse})

	for _, errType := range expectedErrors {
		if errType == nil {
			continue
		}
		status := extractStatusCode(errType, http.StatusInternalServerError)
		errResponse := openapi3.NewResponse().WithDescription(errType.Name())
		errResponse.Content = openapi3.Content{
			"application/json": &openapi3.MediaType{
				Schema: d.schemaRefLocked(errType),
			},
		}
		responses.Set(strconv.Itoa(status), &openapi3.ResponseRef{Value: errResponse})
	}

	if responses.Default() == nil {
		defaultResponse := openapi3.NewResponse().WithDescription("Unexpected error")
		defaultResponse.Content = openapi3.Content{
			"application/json": &openapi3.MediaType{
				Schema: d.schemaRefLocked(typeOf[Error]()),
			},
		}
		responses.Set("default", &openapi3.ResponseRef{Value: defaultResponse})
	}

	op := &openapi3.Operation{
		OperationID: buildOperationID(method, normalizedPath),
		Parameters:  parameters,
		Responses:   responses,
	}

	if requestBody != nil {
		op.RequestBody = requestBody
	}

	pathItem := d.doc.Paths.Value(normalizedPath)
	if pathItem == nil {
		pathItem = &openapi3.PathItem{}
		d.doc.Paths.Set(normalizedPath, pathItem)
	}

	switch strings.ToUpper(method) {
	case http.MethodGet:
		pathItem.Get = op
	case http.MethodPost:
		pathItem.Post = op
	case http.MethodPut:
		pathItem.Put = op
	case http.MethodPatch:
		pathItem.Patch = op
	case http.MethodDelete:
		pathItem.Delete = op
	case http.MethodHead:
		pathItem.Head = op
	case http.MethodOptions:
		pathItem.Options = op
	}
}

func (d *openAPIDocument) buildRequestArtifactsLocked(reqType reflect.Type) (openapi3.Parameters, *openapi3.RequestBodyRef) {
	reqType = derefType(reqType)
	if reqType == nil || reqType.Kind() != reflect.Struct {
		return nil, nil
	}

	var params openapi3.Parameters
	var bodyRequired bool
	var hasBody bool

	for _, field := range exportedFields(reqType) {
		switch {
		case field.Tag.Get("path") != "":
			params = append(params, d.parameterFromFieldLocked(field, "path", field.Tag.Get("path"), true))
		case field.Tag.Get("query") != "":
			required := hasRequiredValidation(field.Tag.Get("validate"))
			params = append(params, d.parameterFromFieldLocked(field, "query", field.Tag.Get("query"), required))
		case field.Tag.Get("header") != "":
			required := hasRequiredValidation(field.Tag.Get("validate"))
			params = append(params, d.parameterFromFieldLocked(field, "header", field.Tag.Get("header"), required))
		default:
			if shouldExcludeFromJSON(field) {
				continue
			}
			_, omitEmpty := parseJSONName(field)
			if hasRequiredValidation(field.Tag.Get("validate")) && !omitEmpty {
				bodyRequired = true
			}
			hasBody = true
		}
	}

	if len(params) > 1 {
		sort.Slice(params, func(i, j int) bool {
			pi := params[i].Value
			pj := params[j].Value
			if pi == nil || pj == nil {
				return i < j
			}
			if pi.In == pj.In {
				return pi.Name < pj.Name
			}
			return pi.In < pj.In
		})
	}

	if !hasBody {
		return params, nil
	}

	schemaRef := d.schemaRefLocked(reqType)

	return params, &openapi3.RequestBodyRef{
		Value: &openapi3.RequestBody{
			Required: bodyRequired,
			Content: openapi3.Content{
				"application/json": &openapi3.MediaType{
					Schema: schemaRef,
				},
			},
		},
	}
}

func (d *openAPIDocument) parameterFromFieldLocked(field reflect.StructField, location, name string, required bool) *openapi3.ParameterRef {
	if name == "" {
		name = field.Name
	}

	return &openapi3.ParameterRef{
		Value: &openapi3.Parameter{
			Name:     name,
			In:       location,
			Required: required || location == "path",
			Schema:   d.inlineSchemaRefLocked(field.Type),
		},
	}
}

func (d *openAPIDocument) inlineSchemaRefLocked(t reflect.Type) *openapi3.SchemaRef {
	t = derefType(t)
	if t == nil {
		return &openapi3.SchemaRef{Value: openapi3.NewObjectSchema()}
	}

	switch t.Kind() {
	case reflect.Struct, reflect.Slice, reflect.Array, reflect.Map:
		return d.schemaRefLocked(t)
	default:
		return d.scalarSchemaRef(t)
	}
}

func (d *openAPIDocument) schemaRefLocked(t reflect.Type) *openapi3.SchemaRef {
	t = derefType(t)
	if t == nil {
		return &openapi3.SchemaRef{Value: openapi3.NewObjectSchema()}
	}

	switch t.Kind() {
	case reflect.Struct:
		if ref, ok := d.typeNames[t]; ok {
			return openapi3.NewSchemaRef("#/components/schemas/"+ref, nil)
		}

		name := schemaComponentName(t)
		d.typeNames[t] = name

		if d.doc.Components.Schemas == nil {
			d.doc.Components.Schemas = openapi3.Schemas{}
		}

		schema := openapi3.NewObjectSchema()
		d.doc.Components.Schemas[name] = &openapi3.SchemaRef{Value: schema}

		for _, field := range exportedFields(t) {
			if shouldExcludeFromJSON(field) {
				continue
			}
			jsonName, omitEmpty := parseJSONName(field)
			if jsonName == "" {
				continue
			}
			schema.Properties[jsonName] = d.inlineSchemaRefLocked(field.Type)
			if hasRequiredValidation(field.Tag.Get("validate")) && !omitEmpty {
				schema.Required = append(schema.Required, jsonName)
			}
		}

		if len(schema.Required) > 1 {
			sort.Strings(schema.Required)
		}

		return openapi3.NewSchemaRef("#/components/schemas/"+name, nil)
	case reflect.Slice, reflect.Array:
		schema := openapi3.NewArraySchema()
		schema.Items = d.schemaRefLocked(t.Elem())
		return &openapi3.SchemaRef{Value: schema}
	case reflect.Map:
		schema := openapi3.NewObjectSchema()
		schema.AdditionalProperties = openapi3.AdditionalProperties{
			Schema: d.schemaRefLocked(t.Elem()),
		}
		return &openapi3.SchemaRef{Value: schema}
	default:
		return d.scalarSchemaRef(t)
	}
}

func (d *openAPIDocument) scalarSchemaRef(t reflect.Type) *openapi3.SchemaRef {
	switch t.Kind() {
	case reflect.String:
		return &openapi3.SchemaRef{Value: openapi3.NewStringSchema()}
	case reflect.Bool:
		return &openapi3.SchemaRef{Value: openapi3.NewBoolSchema()}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		schema := openapi3.NewIntegerSchema()
		schema.Format = intFormat(t.Kind())
		return &openapi3.SchemaRef{Value: schema}
	case reflect.Float32:
		schema := openapi3.NewFloat64Schema()
		schema.Format = "float"
		return &openapi3.SchemaRef{Value: schema}
	case reflect.Float64:
		return &openapi3.SchemaRef{Value: openapi3.NewFloat64Schema()}
	default:
		// Special handling for time.Time
		if t.PkgPath() == "time" && t.Name() == "Time" {
			schema := openapi3.NewStringSchema()
			schema.Format = "date-time"
			return &openapi3.SchemaRef{Value: schema}
		}
		return &openapi3.SchemaRef{Value: openapi3.NewStringSchema()}
	}
}

func (d *openAPIDocument) ServeHTTP(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if d == nil {
		http.Error(w, "openapi unavailable", http.StatusInternalServerError)
		return
	}

	format := strings.ToLower(r.URL.Query().Get("format"))
	switch format {
	case "yaml", "yml":
		bytes, err := d.marshalYAMLLocked()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write(bytes)
	default:
		data, err := d.marshalJSONLocked()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, data, "", "  "); err == nil {
			data = pretty.Bytes()
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(data)
	}
}

func (d *openAPIDocument) marshalJSONLocked() ([]byte, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.doc.MarshalJSON()
}

func (d *openAPIDocument) marshalYAMLLocked() ([]byte, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return yaml.Marshal(d.doc)
}

func (s *Sprout) OpenAPIJSON() ([]byte, error) {
	if s.openapi == nil {
		return nil, fmt.Errorf("openapi not initialized")
	}
	return s.openapi.marshalJSONLocked()
}

func (s *Sprout) OpenAPIYAML() ([]byte, error) {
	if s.openapi == nil {
		return nil, fmt.Errorf("openapi not initialized")
	}
	return s.openapi.marshalYAMLLocked()
}

func derefType(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}

func exportedFields(t reflect.Type) []reflect.StructField {
	var fields []reflect.StructField
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue
		}
		fields = append(fields, field)
	}
	return fields
}

func parseJSONName(field reflect.StructField) (name string, omitEmpty bool) {
	jsonTag := field.Tag.Get("json")
	if jsonTag == "-" {
		return "", false
	}
	if jsonTag == "" {
		return field.Name, false
	}
	parts := strings.Split(jsonTag, ",")
	name = parts[0]
	if name == "" {
		name = field.Name
	}
	if len(parts) > 1 && parts[1] == "omitempty" {
		omitEmpty = true
	}
	return name, omitEmpty
}

func hasRequiredValidation(tag string) bool {
	if tag == "" {
		return false
	}
	for _, token := range strings.FieldsFunc(tag, func(r rune) bool {
		return r == ',' || r == '=' || r == '|'
	}) {
		if token == "required" {
			return true
		}
	}
	return false
}

func schemaComponentName(t reflect.Type) string {
	if t.Name() != "" {
		if pkg := t.PkgPath(); pkg != "" {
			parts := strings.Split(pkg, "/")
			return sanitizeName(parts[len(parts)-1] + "_" + t.Name())
		}
		return sanitizeName(t.Name())
	}
	return sanitizeName(t.String())
}

func sanitizeName(name string) string {
	var builder strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '.', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteRune('_')
		}
	}
	return builder.String()
}

func intFormat(kind reflect.Kind) string {
	switch kind {
	case reflect.Int8, reflect.Uint8, reflect.Int16, reflect.Uint16:
		return "int32"
	case reflect.Int32, reflect.Uint32:
		return "int32"
	case reflect.Int64, reflect.Uint64:
		return "int64"
	default:
		return ""
	}
}

func buildOperationID(method, path string) string {
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		if segment == "" {
			continue
		}
		if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			segment = segment[1 : len(segment)-1]
		}
		segments[i] = capitalize(segment)
	}
	return strings.ToLower(method) + strings.Join(segments, "")
}

func toOpenAPIPath(path string) string {
	var builder strings.Builder
	for i := 0; i < len(path); i++ {
		if path[i] == ':' {
			j := i + 1
			for j < len(path) && (path[j] == '_' || path[j] == '-' || (path[j] >= 'a' && path[j] <= 'z') || (path[j] >= 'A' && path[j] <= 'Z') || (path[j] >= '0' && path[j] <= '9')) {
				j++
			}
			builder.WriteByte('{')
			builder.WriteString(path[i+1 : j])
			builder.WriteByte('}')
			i = j - 1
			continue
		}
		builder.WriteByte(path[i])
	}
	result := builder.String()
	if result == "" {
		return "/"
	}
	return result
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	if len(runes) == 0 {
		return s
	}
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
