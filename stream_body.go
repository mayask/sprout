package sprout

import (
	"io"
	"sync"
)

// StreamBody exposes a request body as a live stream to the route handler.
// Sprout does not pre-read or buffer its contents. The framework closes the
// body after the handler returns; Close is safe to call more than once.
type StreamBody struct {
	reader      io.ReadCloser
	contentType string
	closeOnce   sync.Once
	closeErr    error
}

func newStreamBody(reader io.ReadCloser, contentType string) *StreamBody {
	return &StreamBody{reader: reader, contentType: contentType}
}

func (b *StreamBody) Read(p []byte) (int, error) {
	if b == nil || b.reader == nil {
		return 0, io.EOF
	}
	return b.reader.Read(p)
}

// Close closes the underlying request body. Sprout also invokes Close after
// the handler returns, so handlers only need to call it when ending early.
func (b *StreamBody) Close() error {
	if b == nil || b.reader == nil {
		return nil
	}
	b.closeOnce.Do(func() {
		b.closeErr = b.reader.Close()
	})
	return b.closeErr
}

// ContentType returns the normalized request media type without parameters.
func (b *StreamBody) ContentType() string {
	if b == nil {
		return ""
	}
	return b.contentType
}
