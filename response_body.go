package sprout

import (
	"fmt"
	"net/http"
	"reflect"
)

func explicitResponseBodyCleanup(response any, field *requestBodyField) func() error {
	body, ok := fileBodyFromResponse(response, field)
	if !ok {
		return nil
	}
	return body.Close
}

func fileBodyFromResponse(response any, field *requestBodyField) (*FileBody, bool) {
	if response == nil || field == nil {
		return nil, false
	}
	value := reflect.ValueOf(response)
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil, false
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return nil, false
	}
	bodyValue := value.Field(field.index)
	if bodyValue.IsNil() {
		return nil, false
	}
	body, ok := bodyValue.Interface().(*FileBody)
	return body, ok
}

func writeExplicitResponseBody(
	w http.ResponseWriter,
	req *http.Request,
	response any,
	field *requestBodyField,
	statusCode int,
) (bool, *Error) {
	if field == nil {
		return false, nil
	}
	if statusCode != http.StatusOK {
		return false, &Error{
			Kind:    ErrorKindResponseValidation,
			Message: fmt.Sprintf("FileBody responses require status 200, got %d", statusCode),
		}
	}
	body, ok := fileBodyFromResponse(response, field)
	if !ok {
		return false, &Error{Kind: ErrorKindResponseValidation, Message: "FileBody is nil or unsupported"}
	}

	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", field.contentTypes[0])
	}
	body.serveHTTP(w, req)
	return true, nil
}
