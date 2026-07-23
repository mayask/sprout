package sprout

import (
	"context"
	"fmt"
	"io"
	"mime"
	"sync"
)

// RequestBodyDecoder decodes a consuming request body into target. Params are
// the parsed Content-Type parameters (for example a multipart boundary).
// Cleanup is invoked after the handler returns or immediately on decode error.
// Streaming bodies use StreamBody instead and bypass this decoder lifecycle.
type RequestBodyDecoder func(
	ctx context.Context,
	body io.Reader,
	params map[string]string,
	target any,
) (cleanup func() error, err *Error)

type requestBodyDecoderRegistry struct {
	mu       sync.RWMutex
	decoders map[string]RequestBodyDecoder
}

func newRequestBodyDecoderRegistry() *requestBodyDecoderRegistry {
	return &requestBodyDecoderRegistry{
		decoders: map[string]RequestBodyDecoder{
			defaultRequestContentType: decodeJSONRequestBody,
		},
	}
}

func decodeJSONRequestBody(
	_ context.Context,
	body io.Reader,
	_ map[string]string,
	target any,
) (func() error, *Error) {
	payload, err := io.ReadAll(body)
	if err != nil {
		return nil, &Error{Kind: ErrorKindParse, Message: "failed to read request body", Err: err}
	}
	if len(payload) == 0 {
		return nil, nil
	}
	return nil, decodeJSONBody(payload, target)
}

func (r *requestBodyDecoderRegistry) register(contentType string, decoder RequestBodyDecoder) error {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType == "" {
		return fmt.Errorf("invalid request body decoder content type %q", contentType)
	}
	if decoder == nil {
		return fmt.Errorf("request body decoder for %q is nil", mediaType)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.decoders[mediaType] = decoder
	return nil
}

func (r *requestBodyDecoderRegistry) get(contentType string) RequestBodyDecoder {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.decoders[contentType]
}

// RegisterRequestBodyDecoder registers or replaces the consuming decoder for a
// media type. Call this before registering routes that use that content type.
// StreamBody fields are non-consuming and do not use this registry.
func (s *Sprout) RegisterRequestBodyDecoder(contentType string, decoder RequestBodyDecoder) {
	if s == nil || s.bodyDecoders == nil {
		return
	}
	if err := s.bodyDecoders.register(contentType, decoder); err != nil {
		panic(err)
	}
}
