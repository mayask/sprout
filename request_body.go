package sprout

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"reflect"
	"slices"
	"strings"
)

const defaultRequestContentType = "application/json"

type requestBodyField struct {
	index     int
	fieldType reflect.Type
	// contentTypes lists accepted media types in declaration order.
	contentTypes []string
	required     bool
}

var streamBodyPtrType = reflect.TypeFor[*StreamBody]()
var streamBodyType = reflect.TypeFor[StreamBody]()
var fileBodyPtrType = reflect.TypeFor[*FileBody]()
var fileBodyType = reflect.TypeFor[FileBody]()

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

		rawContentTypes := field.Tag.Get("contentType")
		if strings.TrimSpace(rawContentTypes) == "" {
			rawContentTypes = defaultRequestContentType
		}
		var contentTypes []string
		for _, contentType := range strings.Split(rawContentTypes, ",") {
			mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(contentType))
			if err != nil {
				return nil, fmt.Errorf("request body field %s has invalid contentType %q: %w", field.Name, rawContentTypes, err)
			}
			if !slices.Contains(contentTypes, mediaType) {
				contentTypes = append(contentTypes, mediaType)
			}
		}
		switch field.Type {
		case streamBodyType:
			return nil, fmt.Errorf("body field %s must use *StreamBody, not StreamBody", field.Name)
		case fileBodyType:
			return nil, fmt.Errorf("body field %s must use *FileBody, not FileBody", field.Name)
		}

		result = &requestBodyField{
			index:        i,
			fieldType:    field.Type,
			contentTypes: contentTypes,
			required:     hasRequiredValidation(field.Tag.Get("validate")),
		}
	}
	return result, nil
}

func isStreamBodyType(t reflect.Type) bool {
	return t == streamBodyPtrType
}

func hasBodyTag(field reflect.StructField) bool {
	_, ok := field.Tag.Lookup("body")
	return ok
}

func isFileBodyType(t reflect.Type) bool {
	return t == fileBodyPtrType
}

type prefixedReadCloser struct {
	io.Reader
	io.Closer
}

func bindRequestBody(
	ctx context.Context,
	s *Sprout,
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

	mediaType, mediaParams, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil || mediaType == "" {
		return req.Body.Close, &Error{Kind: ErrorKindParse, Message: "invalid request content type", Err: err}
	}
	if !slices.Contains(field.contentTypes, mediaType) {
		return req.Body.Close, &Error{
			Kind:    ErrorKindParse,
			Message: fmt.Sprintf("unsupported request content type %q; expected one of %q", mediaType, field.contentTypes),
		}
	}

	fieldValue := reqValue.Field(field.index)

	if limit > 0 {
		req.Body = http.MaxBytesReader(w, req.Body, limit)
	}

	// Detect an empty chunked body without buffering the upload. Reinsert the
	// single byte so the handler/decoder observes the original stream intact.
	var first [1]byte
	n, readErr := req.Body.Read(first[:])
	if n == 0 && errors.Is(readErr, io.EOF) {
		return req.Body.Close, nil
	}
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return req.Body.Close, &Error{Kind: ErrorKindParse, Message: "failed to read request body", Err: readErr}
	}
	req.Body = &prefixedReadCloser{
		Reader: io.MultiReader(bytes.NewReader(first[:n]), req.Body),
		Closer: req.Body,
	}
	if isStreamBodyType(field.fieldType) {
		stream := newStreamBody(req.Body, mediaType)
		fieldValue.Set(reflect.ValueOf(stream))
		return stream.Close, nil
	}

	decoder := s.bodyDecoders.get(mediaType)
	if decoder == nil {
		return req.Body.Close, &Error{
			Kind:    ErrorKindParse,
			Message: fmt.Sprintf("no request body decoder registered for content type %q", mediaType),
		}
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

	decoderCleanup, decodeErr := decoder(ctx, req.Body, mediaParams, target)
	cleanup := combineRequestBodyCleanup(decoderCleanup, req.Body.Close)
	return cleanup, decodeErr
}

func combineRequestBodyCleanup(cleanups ...func() error) func() error {
	return func() error {
		var errs []error
		for _, cleanup := range cleanups {
			if cleanup == nil {
				continue
			}
			if err := cleanup(); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}
}
