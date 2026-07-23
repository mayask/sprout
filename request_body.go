package sprout

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"reflect"
	"strings"
)

const defaultRequestContentType = "application/json"

type requestBodyField struct {
	index       int
	fieldType   reflect.Type
	contentType string
	required    bool
}

var streamBodyPtrType = reflect.TypeFor[*StreamBody]()
var streamBodyType = reflect.TypeFor[StreamBody]()

func findRequestBodyField(reqType reflect.Type) (*requestBodyField, error) {
	reqType = derefType(reqType)
	if reqType == nil || reqType.Kind() != reflect.Struct {
		return nil, nil
	}

	var result *requestBodyField
	for i := range reqType.NumField() {
		field := reqType.Field(i)
		if field.PkgPath != "" {
			continue
		}
		if _, ok := field.Tag.Lookup("body"); !ok {
			continue
		}
		if result != nil {
			return nil, fmt.Errorf("request type %s declares multiple body fields", reqType)
		}

		contentType := strings.TrimSpace(field.Tag.Get("contentType"))
		if contentType == "" {
			contentType = defaultRequestContentType
		}
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil {
			return nil, fmt.Errorf("request body field %s has invalid contentType %q: %w", field.Name, contentType, err)
		}

		result = &requestBodyField{
			index:       i,
			fieldType:   field.Type,
			contentType: mediaType,
			required:    hasRequiredValidation(field.Tag.Get("validate")),
		}
	}
	return result, nil
}

func isStreamBodyType(t reflect.Type) bool {
	return t == streamBodyType || t == streamBodyPtrType
}

func hasBodyTag(field reflect.StructField) bool {
	_, ok := field.Tag.Lookup("body")
	return ok
}

func bindRequestBody(
	w http.ResponseWriter,
	req *http.Request,
	reqValue reflect.Value,
	field *requestBodyField,
	limit int64,
) (func() error, *Error) {
	if field == nil {
		return nil, nil
	}

	if req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}

	mediaType, _, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil || mediaType == "" {
		return req.Body.Close, &Error{Kind: ErrorKindParse, Message: "invalid request content type", Err: err}
	}
	if mediaType != field.contentType {
		return req.Body.Close, &Error{
			Kind:    ErrorKindParse,
			Message: fmt.Sprintf("unsupported request content type %q; expected %q", mediaType, field.contentType),
		}
	}

	if limit > 0 {
		req.Body = http.MaxBytesReader(w, req.Body, limit)
	}

	fieldValue := reqValue.Field(field.index)
	if isStreamBodyType(field.fieldType) {
		stream := newStreamBody(req.Body, mediaType)
		if field.fieldType == streamBodyPtrType {
			fieldValue.Set(reflect.ValueOf(stream))
		} else {
			fieldValue.Set(reflect.ValueOf(*stream))
		}
		return stream.Close, nil
	}

	if mediaType != defaultRequestContentType {
		return req.Body.Close, &Error{
			Kind:    ErrorKindParse,
			Message: fmt.Sprintf("no request body decoder registered for content type %q", mediaType),
		}
	}

	body, readErr := io.ReadAll(req.Body)
	if readErr != nil {
		return req.Body.Close, &Error{Kind: ErrorKindParse, Message: "failed to read request body", Err: readErr}
	}
	if len(body) == 0 {
		return req.Body.Close, nil
	}

	var target any
	if fieldValue.Kind() == reflect.Pointer {
		if fieldValue.IsNil() {
			fieldValue.Set(reflect.New(fieldValue.Type().Elem()))
		}
		target = fieldValue.Interface()
	} else {
		target = fieldValue.Addr().Interface()
	}
	if decodeErr := decodeJSONBody(body, target); decodeErr != nil {
		return req.Body.Close, decodeErr
	}
	return req.Body.Close, nil
}
