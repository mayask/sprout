package sprout

import (
	"io"
	"net/http"
	"sync"
	"time"
)

// ReadSeekCloser is the file-like contract required by FileBody.
type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}

// FileBody streams seekable file content through http.ServeContent, preserving
// range and conditional request support. Sprout closes the content after the
// response is served. FileBody request/response fields must use pointer form.
type FileBody struct {
	content   ReadSeekCloser
	name      string
	modTime   time.Time
	closeOnce sync.Once
	closeErr  error
}

// NewFileBody creates a binary response body backed by seekable content.
func NewFileBody(content ReadSeekCloser, name string, modTime time.Time) *FileBody {
	return &FileBody{content: content, name: name, modTime: modTime}
}

func (b *FileBody) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if b == nil || b.content == nil {
		return
	}
	http.ServeContent(w, r, b.name, b.modTime, b.content)
}

// Close closes the underlying content. It is safe to call more than once.
func (b *FileBody) Close() error {
	if b == nil || b.content == nil {
		return nil
	}
	b.closeOnce.Do(func() {
		b.closeErr = b.content.Close()
	})
	return b.closeErr
}
