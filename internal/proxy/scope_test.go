package proxy

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/artilugio0/proxy-vibes/internal/certs"
)

func TestScopeServeHTTP(t *testing.T) {
	rootCA, rootKey, _, _, err := certs.GenerateRootCA()
	if err != nil {
		t.Fatalf("Failed to generate Root CA: %v", err)
	}

	tests := []struct {
		name            string
		isOutOfScope    bool
		expectInFlag    bool
		expectModHeader string
		expectOutFlag   bool
	}{
		{
			name:            "In scope with all pipelines",
			isOutOfScope:    false,
			expectInFlag:    true,
			expectModHeader: "mod1",
			expectOutFlag:   true,
		},
		{
			name:            "Out of scope with all pipelines",
			isOutOfScope:    true,
			expectInFlag:    false,
			expectModHeader: "",
			expectOutFlag:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProxy(rootCA, rootKey)
			inExecuted := false
			outExecuted := false

			p.RequestInPipeline = newReadOnlyPipeline([]ReadOnlyHook[*http.Request]{
				func(req *http.Request) error {
					inExecuted = true
					return nil
				},
			})
			p.RequestModPipeline = newModPipeline([]ModHook[*http.Request]{
				func(req *http.Request) (*http.Request, error) {
					req.Header.Set("X-Mod", "mod1")
					return req, nil
				},
			})
			p.RequestOutPipeline = newReadOnlyPipeline([]ReadOnlyHook[*http.Request]{
				func(req *http.Request) error {
					outExecuted = true
					return nil
				},
			})
			p.ResponseInPipeline = newReadOnlyPipeline([]ReadOnlyHook[*http.Response]{
				func(resp *http.Response) error {
					inExecuted = true
					return nil
				},
			})
			p.ResponseModPipeline = newModPipeline([]ModHook[*http.Response]{
				func(resp *http.Response) (*http.Response, error) {
					resp.Header.Set("X-Mod", "mod1")
					return resp, nil
				},
			})
			p.ResponseOutPipeline = newReadOnlyPipeline([]ReadOnlyHook[*http.Response]{
				func(resp *http.Response) error {
					outExecuted = true
					return nil
				},
			})
			p.InScopeFunc = func(*http.Request) bool { return !tt.isOutOfScope }

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Original", "original")
				w.Write([]byte("Success"))
			}))
			defer server.Close()

			p.Client = &http.Client{
				Transport: &http.Transport{},
			}

			req := httptest.NewRequest("GET", server.URL, nil)
			w := httptest.NewRecorder()

			p.ServeHTTP(w, req)

			resp := w.Result()
			body, _ := io.ReadAll(resp.Body)
			response := string(body)

			if resp.StatusCode != http.StatusOK {
				t.Errorf("Expected status 200, got %d", resp.StatusCode)
			}
			if !strings.Contains(response, "Success") {
				t.Errorf("Expected response 'Success', got %s", response)
			}

			if inExecuted != tt.expectInFlag {
				t.Errorf("Expected inExecuted to be %v, got %v", tt.expectInFlag, inExecuted)
			}
			if outExecuted != tt.expectOutFlag {
				t.Errorf("Expected outExecuted to be %v, got %v", tt.expectOutFlag, outExecuted)
			}
			if resp.Header.Get("X-Mod") != tt.expectModHeader {
				t.Errorf("Expected X-Mod header to be %q, got %q", tt.expectModHeader, resp.Header.Get("X-Mod"))
			}
			if tt.isOutOfScope && resp.Header.Get("X-Original") != "original" {
				t.Errorf("Expected X-Original header unchanged for out-of-scope, got %q", resp.Header.Get("X-Original"))
			}
		})
	}
}

func TestScopeHandleConnect(t *testing.T) {
	rootCA, rootKey, certPEM, _, err := certs.GenerateRootCA()
	if err != nil {
		t.Fatalf("Failed to generate Root CA: %v", err)
	}
	certBlock, _ := pem.Decode([]byte(certPEM))
	if certBlock == nil {
		t.Fatal("Failed to decode Root CA PEM")
	}
	parsedRootCA, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		t.Fatalf("Failed to parse Root CA: %v", err)
	}

	tests := []struct {
		name            string
		isOutOfScope    bool
		expectInFlag    bool
		expectModHeader string
		expectOutFlag   bool
	}{
		{
			name:            "In scope with all pipelines",
			isOutOfScope:    false,
			expectInFlag:    true,
			expectModHeader: "mod1",
			expectOutFlag:   true,
		},
		{
			name:            "Out of scope with all pipelines",
			isOutOfScope:    true,
			expectInFlag:    false,
			expectModHeader: "",
			expectOutFlag:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProxy(rootCA, rootKey)
			inExecuted := false
			outExecuted := false

			p.RequestInPipeline = newReadOnlyPipeline([]ReadOnlyHook[*http.Request]{
				func(req *http.Request) error {
					inExecuted = true
					return nil
				},
			})
			p.RequestModPipeline = newModPipeline([]ModHook[*http.Request]{
				func(req *http.Request) (*http.Request, error) {
					req.Header.Set("X-Mod", "mod1")
					return req, nil
				},
			})
			p.RequestOutPipeline = newReadOnlyPipeline([]ReadOnlyHook[*http.Request]{
				func(req *http.Request) error {
					outExecuted = true
					return nil
				},
			})
			p.ResponseInPipeline = newReadOnlyPipeline([]ReadOnlyHook[*http.Response]{
				func(resp *http.Response) error {
					inExecuted = true
					return nil
				},
			})
			p.ResponseModPipeline = newModPipeline([]ModHook[*http.Response]{
				func(resp *http.Response) (*http.Response, error) {
					resp.Header.Set("X-Mod", "mod1")
					return resp, nil
				},
			})
			p.ResponseOutPipeline = newReadOnlyPipeline([]ReadOnlyHook[*http.Response]{
				func(resp *http.Response) error {
					outExecuted = true
					return nil
				},
			})
			p.InScopeFunc = func(*http.Request) bool { return !tt.isOutOfScope }

			serverCert, err := certs.GenerateCert([]string{"localhost", "127.0.0.1"}, rootCA, rootKey)
			if err != nil {
				t.Fatalf("Failed to generate server certificate: %v", err)
			}
			destServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Original", "original")
				w.Write([]byte("Success"))
			}))
			destServer.TLS.Certificates = []tls.Certificate{*serverCert}
			defer destServer.Close()

			destAddr := destServer.Listener.Addr().String()

			proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				p.HandleConnect(w, r)
			}))
			defer proxyServer.Close()

			proxyURL, _ := url.Parse("http://" + proxyServer.Listener.Addr().String())
			client := &http.Client{
				Transport: &http.Transport{
					Proxy: http.ProxyURL(proxyURL),
					TLSClientConfig: &tls.Config{
						RootCAs: getRootCAPool(parsedRootCA), // Use parsed Root CA
					},
				},
			}

			resp, err := client.Get("https://localhost:" + destAddr[strings.LastIndex(destAddr, ":")+1:])
			if err != nil {
				t.Fatalf("Failed to perform request through proxy: %v", err)
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("Failed to read response body: %v", err)
			}
			response := string(body)

			if resp.StatusCode != http.StatusOK {
				t.Errorf("Expected status 200, got %d", resp.StatusCode)
			}
			if !strings.Contains(response, "Success") {
				t.Errorf("Expected response 'Success', got %s", response)
			}

			if inExecuted != tt.expectInFlag {
				t.Errorf("Expected inExecuted to be %v, got %v", tt.expectInFlag, inExecuted)
			}
			if outExecuted != tt.expectOutFlag {
				t.Errorf("Expected outExecuted to be %v, got %v", tt.expectOutFlag, outExecuted)
			}
			if resp.Header.Get("X-Mod") != tt.expectModHeader {
				t.Errorf("Expected X-Mod header to be %q, got %q", tt.expectModHeader, resp.Header.Get("X-Mod"))
			}
			if tt.isOutOfScope && resp.Header.Get("X-Original") != "original" {
				t.Errorf("Expected X-Original header unchanged for out-of-scope, got %q", resp.Header.Get("X-Original"))
			}
		})
	}
}
