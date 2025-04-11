package proxy

import (
	"bytes"
	"io"
	"log"
	"net/http"
)

// BodyWrapper is a type that wraps a byte array and implements io.ReadCloser
type BodyWrapper struct {
	data   []byte        // The underlying byte array
	reader *bytes.Reader // The reader for the byte array
}

// NewBodyWrapper creates a new BodyWrapper from a byte slice
func NewBodyWrapper(data []byte) *BodyWrapper {
	return &BodyWrapper{
		data:   data,
		reader: bytes.NewReader(data),
	}
}

// Read implements the io.Reader interface
func (b *BodyWrapper) Read(p []byte) (n int, err error) {
	return b.reader.Read(p)
}

// Close implements the io.Closer interface (no-op in this case)
func (b *BodyWrapper) Close() error {
	// Since we're using bytes.Reader, there's nothing to close,
	// but we implement this for io.ReadCloser compatibility
	return nil
}

// ShallowClone creates a new BodyWrapper instance with the same underlying
// byte array and a fresh reader reset to the start
func (b *BodyWrapper) ShallowClone() *BodyWrapper {
	return &BodyWrapper{
		data:   b.data,                  // Reference the same byte array (shallow copy)
		reader: bytes.NewReader(b.data), // New reader starting at position 0
	}
}

// Reset resets the reader's position to the beginning of the byte array
func (b *BodyWrapper) Reset() {
	b.reader.Seek(0, io.SeekStart)
}

// cloneRequest creates a deep copy of an HTTP request
func cloneRequest(req *http.Request) *http.Request {
	r := new(http.Request)
	*r = *req

	r.Header = make(http.Header)
	for k, v := range req.Header {
		r.Header[k] = append([]string(nil), v...)
	}

	if req.Body != nil {
		if wrapper, ok := req.Body.(*BodyWrapper); ok {
			r.Body = wrapper.ShallowClone()
		} else {
			bodyBytes, err := io.ReadAll(req.Body)
			if err != nil {
				log.Printf("Error reading request body: %v", err)
			}
			newBody := NewBodyWrapper(bodyBytes)
			req.Body = newBody
			r.Body = newBody.ShallowClone()
			r.ContentLength = req.ContentLength
		}
	} else {
		r.Body = nil
	}

	return r
}

// cloneResponse creates a deep copy of an HTTP response
func cloneResponse(resp *http.Response) *http.Response {
	r := new(http.Response)
	*r = *resp

	r.Header = make(http.Header)
	for k, v := range resp.Header {
		r.Header[k] = append([]string(nil), v...)
	}

	if resp.Body != nil {
		if wrapper, ok := resp.Body.(*BodyWrapper); ok {
			r.Body = wrapper.ShallowClone()
		} else {
			bodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				log.Printf("Error reading response body: %v", err)
			}
			newBody := NewBodyWrapper(bodyBytes)
			resp.Body = newBody
			r.Body = newBody.ShallowClone()
		}
	} else {
		r.Body = nil
	}

	return r
}
