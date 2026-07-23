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
	resolver  OpenAPISchemaResolver
}

// OpenAPIInfo configures high-level OpenAPI document metadata.
type OpenAPIInfo struct {
	Title       string
	Version     string
	Description string
	Terms       string
	Contact     *OpenAPIContact
	License     *OpenAPILicense
	Servers     []OpenAPIServer
}

// OpenAPIContact describes the API contact information.
type OpenAPIContact struct {
	Name  string
	URL   string
	Email string
}

// OpenAPILicense describes the API license information.
type OpenAPILicense struct {
	Name string
	URL  string
}

// OpenAPIServer represents a server entry in the OpenAPI document.
type OpenAPIServer struct {
	URL         string
	Description string
}

// WithOpenAPIInfo configures the router's OpenAPI metadata.
func WithOpenAPIInfo(info OpenAPIInfo) Option {
	return func(cfg *Config) {
		cfg.openapiInfo = cloneOpenAPIInfo(info)
	}
}

func cloneOpenAPIInfo(info OpenAPIInfo) *OpenAPIInfo {
	clone := info
	if info.Contact != nil {
		contactCopy := *info.Contact
		clone.Contact = &contactCopy
	}
	if info.License != nil {
		licenseCopy := *info.License
		clone.License = &licenseCopy
	}
	if len(info.Servers) > 0 {
		clone.Servers = append([]OpenAPIServer(nil), info.Servers...)
	}
	return &clone
}

func newOpenAPIDocument(info *OpenAPIInfo, resolver OpenAPISchemaResolver) *openAPIDocument {
	components := openapi3.NewComponents()
	components.Schemas = openapi3.Schemas{}

	docInfo := &openapi3.Info{
		Title:   "Sprout API",
		Version: "1.0.0",
	}

	if info != nil {
		if info.Title != "" {
			docInfo.Title = info.Title
		}
		if info.Version != "" {
			docInfo.Version = info.Version
		}
		if info.Description != "" {
			docInfo.Description = info.Description
		}
		if info.Terms != "" {
			docInfo.TermsOfService = info.Terms
		}
		if info.Contact != nil {
			docInfo.Contact = &openapi3.Contact{
				Name:  info.Contact.Name,
				URL:   info.Contact.URL,
				Email: info.Contact.Email,
			}
		}
		if info.License != nil {
			docInfo.License = &openapi3.License{
				Name: info.License.Name,
				URL:  info.License.URL,
			}
		}
	}

	doc := &openapi3.T{
		OpenAPI:    "3.0.3",
		Info:       docInfo,
		Paths:      openapi3.NewPaths(),
		Components: &components,
	}

	if info != nil && len(info.Servers) > 0 {
		doc.Servers = make(openapi3.Servers, len(info.Servers))
		for i, server := range info.Servers {
			doc.Servers[i] = &openapi3.Server{
				URL:         server.URL,
				Description: server.Description,
			}
		}
	}

	return &openAPIDocument{
		doc:       doc,
		typeNames: make(map[reflect.Type]string),
		resolver:  resolver,
	}
}

func (d *openAPIDocument) setResolver(r OpenAPISchemaResolver) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.resolver = r
}

// resolvedSchemaRefLocked calls the resolver, if set, to produce a schema for
// the given type. The caller must already hold d.mu (route registration path).
// The resolver must be a pure function and MUST NOT call back into Sprout
// APIs or it will deadlock on d.mu.
//
// For named types (t.Name() != ""), the returned inline schema is promoted to
// a shared component in doc.Components.Schemas and subsequent lookups return a
// $ref. This mirrors the existing struct-component pattern and avoids inlining
// the same schema (e.g. a large enum) at every usage site.
func (d *openAPIDocument) resolvedSchemaRefLocked(t reflect.Type) *openapi3.SchemaRef {
	if d.resolver == nil {
		return nil
	}

	// Return existing component ref for already-promoted named types.
	if t.Name() != "" {
		if name, ok := d.typeNames[t]; ok {
			return openapi3.NewSchemaRef("#/components/schemas/"+name, nil)
		}
	}

	schema := d.resolver(t)
	if schema == nil {
		return nil
	}

	// Promote named types to shared components so the schema is defined once.
	if t.Name() != "" && t.PkgPath() != "" {
		name := schemaComponentName(t)
		d.typeNames[t] = name
		if d.doc.Components.Schemas == nil {
			d.doc.Components.Schemas = openapi3.Schemas{}
		}
		d.doc.Components.Schemas[name] = schema
		return openapi3.NewSchemaRef("#/components/schemas/"+name, nil)
	}

	return schema
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

	explicitBody, err := findRequestBodyField(reqType)
	if err != nil {
		panic(err)
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
		case hasBodyTag(field):
			continue
		default:
			if shouldExcludeFromJSON(field) {
				continue
			}
			tagInfo := parseJSONTag(field)
			if tagInfo.Name == "" || isUnwrapField(field) {
				continue
			}
			if hasRequiredValidation(field.Tag.Get("validate")) && !tagInfo.OmitEmpty {
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

	if explicitBody != nil {
		return params, &openapi3.RequestBodyRef{
			Value: &openapi3.RequestBody{
				Required: explicitBody.required,
				Content: openapi3.Content{
					explicitBody.contentType: &openapi3.MediaType{
						Schema: d.schemaRefLocked(explicitBody.fieldType),
					},
				},
			},
		}
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

	if schema := d.resolvedSchemaRefLocked(t); schema != nil {
		return schema
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
	if t == streamBodyType {
		schema := openapi3.NewStringSchema()
		schema.Format = "binary"
		return &openapi3.SchemaRef{Value: schema}
	}

	if schema := d.resolvedSchemaRefLocked(t); schema != nil {
		return schema
	}
	switch t.Kind() {
	case reflect.Struct:
		// Built-in: time.Time serializes as an RFC 3339 string, not an object.
		if t.PkgPath() == "time" && t.Name() == "Time" {
			schema := openapi3.NewStringSchema()
			schema.Format = "date-time"
			return &openapi3.SchemaRef{Value: schema}
		}
		if unwrapType, ok := unwrapJSONFieldType(t); ok {
			return d.schemaRefLocked(unwrapType)
		}

		// Alias optimization: if the struct has only one anonymous embedded
		// struct field and no extra named fields, it serializes identically to
		// the embedded type (Go's encoding/json flattens). Return a $ref to
		// the embedded type's schema instead of creating a duplicate component.
		// This avoids wrapper types like CreateWalletResponse (embeds
		// WalletResponse with http:"status=201") generating duplicate schemas.
		// Error types (4xx/5xx) are excluded because their distinct component
		// names carry semantic meaning for error discrimination in generated
		// clients (e.g. Orval produces separate ErrorsBadRequestError types).
		fields := exportedFields(t)
		if len(fields) == 1 && fields[0].Anonymous {
			embeddedType := derefType(fields[0].Type)
			if embeddedType.Kind() == reflect.Struct && embeddedType != t {
				status := extractStatusCode(t, http.StatusOK)
				if status < 400 {
					return d.schemaRefLocked(embeddedType)
				}
			}
		}

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
			// Flatten anonymous embedded struct fields into the parent schema,
			// mirroring toJSONMap's behavior. This must happen before
			// shouldExcludeFromJSON because embedded structs often carry
			// http/status tags that would otherwise hide their fields.
			if field.Anonymous {
				embeddedType := derefType(field.Type)
				if embeddedType.Kind() == reflect.Struct {
					for _, embeddedField := range exportedFields(embeddedType) {
						if shouldExcludeFromJSON(embeddedField) {
							continue
						}
						embeddedTagInfo := parseJSONTag(embeddedField)
						if embeddedTagInfo.Name == "" || isUnwrapField(embeddedField) {
							continue
						}
						schema.Properties[embeddedTagInfo.Name] = d.inlineSchemaRefLocked(embeddedField.Type)
						if hasRequiredValidation(embeddedField.Tag.Get("validate")) && !embeddedTagInfo.OmitEmpty {
							schema.Required = append(schema.Required, embeddedTagInfo.Name)
						}
					}
					continue
				}
			}

			if shouldExcludeFromJSON(field) {
				continue
			}
			tagInfo := parseJSONTag(field)
			if tagInfo.Name == "" || isUnwrapField(field) {
				continue
			}
			schema.Properties[tagInfo.Name] = d.inlineSchemaRefLocked(field.Type)
			if hasRequiredValidation(field.Tag.Get("validate")) && !tagInfo.OmitEmpty {
				schema.Required = append(schema.Required, tagInfo.Name)
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

func exportedFields(t reflect.Type) []reflect.StructField {
	var fields []reflect.StructField
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		// Anonymous embedded fields are included even if the embedded type
		// is unexported — Go's encoding/json promotes the embedded type's
		// exported fields to the parent, and schema generation must match.
		if field.PkgPath != "" && !field.Anonymous {
			continue
		}
		fields = append(fields, field)
	}
	return fields
}

func derefType(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
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
