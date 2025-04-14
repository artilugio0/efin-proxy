package httpbytes

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCloneRequest tests the cloneRequest function
func TestCloneRequest(t *testing.T) {
	// Test with no body (GET request)
	req := httptest.NewRequest("GET", "http://example.com", nil)
	req.Header.Set("X-Test", "value")
	cloned := CloneRequest(req)

	if cloned == req {
		t.Errorf("cloneRequest should return a new request, got same pointer: %p", req)
	}
	if cloned.URL.String() != req.URL.String() {
		t.Errorf("Expected URL %s, got %s", req.URL.String(), cloned.URL.String())
	}
	if cloned.Header.Get("X-Test") != "value" {
		t.Errorf("Expected header X-Test 'value', got %s", cloned.Header.Get("X-Test"))
	}
	if cloned.Body == nil {
		t.Error("Expected non-nil body (empty reader) for GET request, got nil")
	}
	clonedBody, _ := io.ReadAll(cloned.Body)
	if len(clonedBody) != 0 {
		t.Errorf("Expected empty body for GET request, got %s", string(clonedBody))
	}

	// Test with body (POST request)
	body := "test body"
	reqWithBody := httptest.NewRequest("POST", "http://example.com", strings.NewReader(body))
	clonedWithBody := CloneRequest(reqWithBody)

	if clonedWithBody == reqWithBody {
		t.Errorf("cloneRequest should return a new request, got same pointer: %p", reqWithBody)
	}
	clonedBody, _ = io.ReadAll(clonedWithBody.Body)
	if string(clonedBody) != body {
		t.Errorf("Expected body %s, got %s", body, string(clonedBody))
	}
	if clonedWithBody.ContentLength != int64(len(body)) {
		t.Errorf("Expected ContentLength %d, got %d", len(body), clonedWithBody.ContentLength)
	}

	// Verify original body is preserved
	origBody, _ := io.ReadAll(reqWithBody.Body)
	if string(origBody) != body {
		t.Errorf("Original body should be preserved, got %s", string(origBody))
	}
}
