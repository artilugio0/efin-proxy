package hooks

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/artilugio0/proxy-vibes/internal/proxy"
)

func TestRawRequestBytes(t *testing.T) {
	tests := []struct {
		name    string
		req     *http.Request
		want    string
		hasBody bool
	}{
		{
			name:    "Simple GET request",
			req:     httptest.NewRequest("GET", "http://example.com/path", nil),
			want:    "GET /path HTTP/1.1\r\nHost: example.com\r\n\r\n",
			hasBody: false,
		},
		{
			name:    "POST request with body",
			req:     httptest.NewRequest("POST", "http://example.com/post", strings.NewReader("data")),
			want:    "POST /post HTTP/1.1\r\nHost: example.com\r\n\r\ndata",
			hasBody: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RawRequestBytes(tt.req)
			if err != nil {
				t.Errorf("RawRequestBytes() error = %v", err)
				return
			}
			if string(got) != tt.want {
				t.Errorf("RawRequestBytes() = %q, want %q", got, tt.want)
			}
			if tt.hasBody && tt.req.Body != nil {
				bodyBytes, _ := io.ReadAll(tt.req.Body)
				if len(bodyBytes) == 0 {
					t.Errorf("RawRequestBytes() did not restore request body")
				}
			}
		})
	}
}

func TestRawResponseBytes(t *testing.T) {
	tests := []struct {
		name    string
		resp    *http.Response
		want    string
		hasBody bool
	}{
		{
			name: "Simple 200 OK response",
			resp: &http.Response{
				StatusCode: 200,
				Proto:      "HTTP/1.1",
				Header:     http.Header{"Content-Type": []string{"text/plain"}},
				Body:       io.NopCloser(strings.NewReader("")),
			},
			want:    "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\n",
			hasBody: false,
		},
		{
			name: "Response with body",
			resp: &http.Response{
				StatusCode: 201,
				Proto:      "HTTP/1.1",
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"message": "created"}`)),
			},
			want:    "HTTP/1.1 201 Created\r\nContent-Type: application/json\r\n\r\n{\"message\": \"created\"}",
			hasBody: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RawResponseBytes(tt.resp)
			if err != nil {
				t.Errorf("RawResponseBytes() error = %v", err)
				return
			}
			if string(got) != tt.want {
				t.Errorf("RawResponseBytes() = %q, want %q", got, tt.want)
			}
			if tt.hasBody && tt.resp.Body != nil {
				bodyBytes, _ := io.ReadAll(tt.resp.Body)
				if len(bodyBytes) == 0 {
					t.Errorf("RawResponseBytes() did not restore response body")
				}
			}
		})
	}
}

func TestLogRawRequest(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	req := httptest.NewRequest("GET", "https://test.host.com/path", nil)
	req = proxy.SetRequestID(req, "test-request-id")
	LogRawRequest(req)

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	got := buf.String()

	want := "---------- PROXY-VIBES REQUEST START: test-request-id ----------\r\nGET /path HTTP/1.1\r\nHost: test.host.com\r\n\r\n---------- PROXY-VIBES REQUEST END: test-request-id ----------\r\n"

	if got != want {
		t.Errorf("LogRawRequest() output = %q, want %q", got, want)
	}
}

func TestLogRawResponse(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Create a sample response
	resp := &http.Response{
		StatusCode: 200,
		Proto:      "HTTP/1.1",
		Header:     http.Header{"Content-Type": []string{"text/plain"}},
		Body:       io.NopCloser(strings.NewReader("Success")),
	}
	resp.Request = httptest.NewRequest("GET", "https://test.host.com", nil)
	resp.Request = proxy.SetRequestID(resp.Request, "test-response-id")
	LogRawResponse(resp)

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	got := buf.String()

	want := "---------- PROXY-VIBES RESPONSE START: test-response-id ----------\r\nHTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\nSuccess---------- PROXY-VIBES RESPONSE END: test-response-id ----------\r\n"

	if got != want {
		t.Errorf("LogRawResponse() output = %q, want %q", got, want)
	}
}

func TestSaveHooks(t *testing.T) {
	tests := []struct {
		name string
		dir  string
	}{
		{
			name: "Default directory (empty)",
			dir:  "",
		},
		{
			name: "Custom directory",
			dir:  "test-dir",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory
			tempDir, err := os.MkdirTemp("", "proxy-vibes-test")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tempDir)

			// Use tempDir as base, adjust dir accordingly
			saveDir := tt.dir
			if saveDir == "" {
				saveDir = tempDir
			} else {
				saveDir = filepath.Join(tempDir, tt.dir)
				if err := os.MkdirAll(saveDir, 0755); err != nil {
					t.Fatalf("Failed to create save dir: %v", err)
				}
			}

			// Get save hooks
			saveRequest, saveResponse := NewFileSaveHooks(saveDir)

			// Create and save request
			req := httptest.NewRequest("GET", "https://test.host.com/path", nil)
			req = proxy.SetRequestID(req, "test-request-id")
			if err := saveRequest(req); err != nil {
				t.Errorf("saveRequest() error = %v", err)
			}

			// Create and save response
			resp := &http.Response{
				StatusCode: 200,
				Proto:      "HTTP/1.1",
				Header:     http.Header{"Content-Type": []string{"text/plain"}},
				Body:       io.NopCloser(strings.NewReader("Success")),
			}
			resp.Request = httptest.NewRequest("GET", "https://test.host.com", nil)
			resp.Request = proxy.SetRequestID(resp.Request, "test-response-id")
			if err := saveResponse(resp); err != nil {
				t.Errorf("saveResponse() error = %v", err)
			}

			// Check request file
			reqFile := filepath.Join(saveDir, "request-test-request-id.txt")
			reqContent, err := os.ReadFile(reqFile)
			if err != nil {
				t.Errorf("Failed to read request file %s: %v", reqFile, err)
			}
			wantReq := "GET /path HTTP/1.1\r\nHost: test.host.com\r\n\r\n"
			if string(reqContent) != wantReq {
				t.Errorf("Request file content = %q, want %q", reqContent, wantReq)
			}

			// Check response file
			respFile := filepath.Join(saveDir, "response-test-response-id.txt")
			respContent, err := os.ReadFile(respFile)
			if err != nil {
				t.Errorf("Failed to read response file %s: %v", respFile, err)
			}
			wantResp := "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\nSuccess"
			if string(respContent) != wantResp {
				t.Errorf("Response file content = %q, want %q", respContent, wantResp)
			}
		})
	}
}
