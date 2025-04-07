package hooks

import (
	"bytes"
	"fmt"
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
		wantErr bool
	}{
		{
			name:    "Simple GET request",
			req:     httptest.NewRequest("GET", "http://example.com/path", nil),
			want:    "GET /path HTTP/1.1\r\nHost: example.com\r\n\r\n",
			hasBody: false,
			wantErr: false,
		},
		{
			name:    "POST request with body",
			req:     httptest.NewRequest("POST", "http://example.com/post", strings.NewReader("data")),
			want:    "POST /post HTTP/1.1\r\nHost: example.com\r\n\r\ndata",
			hasBody: true,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RawRequestBytes(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("RawRequestBytes() error = %v, wantErr %v", err, tt.wantErr)
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
		wantErr bool
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
			wantErr: false,
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
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RawResponseBytes(tt.resp)
			if (err != nil) != tt.wantErr {
				t.Errorf("RawResponseBytes() error = %v, wantErr %v", err, tt.wantErr)
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
	tests := []struct {
		name    string
		setup   func(*proxy.Proxy)
		want    string
		wantErr bool
	}{
		{
			name: "Request with ID",
			setup: func(p *proxy.Proxy) {
				p.RequestInPipeline = append(p.RequestInPipeline, LogRawRequest)
			},
			want:    "---------- PROXY-VIBES REQUEST START: [UUID] ----------\r\nGET /path HTTP/1.1\r\nHost: [HOST]\r\n\r\n---------- PROXY-VIBES REQUEST END: [UUID] ----------\r\n",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := proxy.NewProxy(nil, nil)
			var capturedRequestID string

			p.RequestInPipeline = append(p.RequestInPipeline, func(req *http.Request) error {
				capturedRequestID = proxy.GetRequestID(req)
				return nil
			})
			tt.setup(p)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("Success"))
			}))
			defer server.Close()

			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			req := httptest.NewRequest("GET", server.URL+"/path", nil)
			recorder := httptest.NewRecorder()
			p.ServeHTTP(recorder, req)

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			io.Copy(&buf, r)
			got := buf.String()

			if capturedRequestID == "" {
				t.Errorf("Expected request ID to be set, got empty string")
			}
			host := req.URL.Host
			expected := strings.ReplaceAll(tt.want, "[UUID]", capturedRequestID)
			expected = strings.ReplaceAll(expected, "[HOST]", host)
			if got != expected {
				t.Errorf("LogRawRequest() output = %q, want %q", got, expected)
			}
		})
	}
}

func TestLogRawResponse(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*proxy.Proxy)
		wantErr bool
	}{
		{
			name: "Response with ID",
			setup: func(p *proxy.Proxy) {
				p.ResponseInPipeline = append(p.ResponseInPipeline, LogRawResponse)
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := proxy.NewProxy(nil, nil)
			var capturedResponseID string

			p.ResponseInPipeline = append(p.ResponseInPipeline, func(resp *http.Response) error {
				capturedResponseID = proxy.GetResponseID(resp)
				return nil
			})
			tt.setup(p)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				w.Write([]byte("Success"))
			}))
			defer server.Close()

			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			req := httptest.NewRequest("GET", server.URL, nil)
			recorder := httptest.NewRecorder()
			p.ServeHTTP(recorder, req)
			resp := recorder.Result()

			w.Close()
			os.Stdout = oldStdout

			if resp.StatusCode != http.StatusOK {
				t.Errorf("Expected status 200, got %d", resp.StatusCode)
			}

			var buf bytes.Buffer
			io.Copy(&buf, r)
			got := buf.String()

			if capturedResponseID == "" {
				t.Errorf("Expected response ID to be set, got empty string")
			}

			expectedPrefix := fmt.Sprintf("---------- PROXY-VIBES RESPONSE START: %s ----------\r\n", capturedResponseID)
			expectedSuffix := fmt.Sprintf("---------- PROXY-VIBES RESPONSE END: %s ----------\r\n", capturedResponseID)
			expectedContains := []string{
				"HTTP/1.1 200 OK\r\n",
				"Content-Type: text/plain\r\n",
				"Success",
			}

			if !strings.HasPrefix(got, expectedPrefix) {
				t.Errorf("LogRawResponse() output prefix = %q, want %q", got[:len(expectedPrefix)], expectedPrefix)
			}
			if !strings.HasSuffix(got, expectedSuffix) {
				t.Errorf("LogRawResponse() output suffix = %q, want %q", got[len(got)-len(expectedSuffix):], expectedSuffix)
			}
			for _, content := range expectedContains {
				if !strings.Contains(got, content) {
					t.Errorf("LogRawResponse() output missing %q, got %q", content, got)
				}
			}
		})
	}
}

func TestSaveHooks(t *testing.T) {
	tests := []struct {
		name    string
		dir     string
		wantErr bool
	}{
		{
			name:    "Default directory (empty)",
			dir:     "",
			wantErr: false,
		},
		{
			name:    "Custom directory",
			dir:     "test-dir",
			wantErr: false,
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

			p := proxy.NewProxy(nil, nil)
			var capturedRequestID, capturedResponseID string

			// Get save hooks
			saveRequest, saveResponse := NewFileSaveHooks(saveDir)

			// Capture IDs
			p.RequestInPipeline = append(p.RequestInPipeline, func(req *http.Request) error {
				capturedRequestID = proxy.GetRequestID(req)
				return nil
			}, saveRequest)
			p.ResponseInPipeline = append(p.ResponseInPipeline, func(resp *http.Response) error {
				capturedResponseID = proxy.GetResponseID(resp)
				return nil
			}, saveResponse)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				w.Write([]byte("Success"))
			}))
			defer server.Close()

			req := httptest.NewRequest("GET", server.URL+"/path", nil)
			recorder := httptest.NewRecorder()
			p.ServeHTTP(recorder, req)
			resp := recorder.Result()

			// Check request file
			reqFile := filepath.Join(saveDir, fmt.Sprintf("request-%s.txt", capturedRequestID))
			reqContent, err := os.ReadFile(reqFile)
			if err != nil {
				t.Errorf("Failed to read request file %s: %v", reqFile, err)
			}
			expectedReq, _ := RawRequestBytes(req)
			reqLines := strings.Split(string(reqContent), "\r\n")
			expectedReqLines := strings.Split(string(expectedReq), "\r\n")
			for _, expectedLine := range expectedReqLines {
				if expectedLine == "" {
					continue // Skip empty lines (e.g., between headers and body)
				}
				found := false
				for _, line := range reqLines {
					if line == expectedLine {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Request file %s missing line %q, got %q", reqFile, expectedLine, reqContent)
				}
			}

			// Check response file
			respFile := filepath.Join(saveDir, fmt.Sprintf("response-%s.txt", capturedResponseID))
			respContent, err := os.ReadFile(respFile)
			if err != nil {
				t.Errorf("Failed to read response file %s: %v", respFile, err)
			}
			expectedResp, _ := RawResponseBytes(resp)
			respLines := strings.Split(string(respContent), "\r\n")
			expectedRespLines := strings.Split(string(expectedResp), "\r\n")
			for _, expectedLine := range expectedRespLines {
				if expectedLine == "" {
					continue // Skip empty lines
				}
				found := false
				for _, line := range respLines {
					if line == expectedLine {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Response file %s missing line %q, got %q", respFile, expectedLine, respContent)
				}
			}
		})
	}
}
