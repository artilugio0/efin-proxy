package proxy

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/artilugio0/proxy-vibes/internal/certs"
)

// TestNewProxy tests the NewProxy constructor
func TestNewProxy(t *testing.T) {
	rootCA, rootKey, _, _, err := certs.GenerateRootCA()
	if err != nil {
		t.Fatalf("Failed to generate Root CA: %v", err)
	}

	p := NewProxy(rootCA, rootKey)

	if p.Client == nil {
		t.Error("Client should be initialized")
	}
	if p.CertCache == nil {
		t.Error("CertCache should be initialized")
	}
	if p.RootCA != rootCA {
		t.Error("RootCA should match the provided rootCA")
	}
	if p.RootKey != rootKey {
		t.Error("RootKey should match the provided rootKey")
	}
	if !p.Client.Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify {
		t.Error("TLSClientConfig.InsecureSkipVerify should be true")
	}
}

// TestServeHTTP tests the ServeHTTP method
func TestServeHTTP(t *testing.T) {
	_, rootKey, _, _, err := certs.GenerateRootCA() // Discard rootCA with _
	if err != nil {
		t.Fatalf("Failed to generate Root CA: %v", err)
	}

	p := NewProxy(nil, rootKey) // Pass nil for rootCA since it’s not used
	p.RequestModPipeline = append(p.RequestModPipeline, func(req *http.Request) (*http.Request, error) {
		req.Header.Set("X-Modified", "true")
		return req, nil
	})

	// Mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Modified") != "true" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
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
}

// TestCloneRequest tests the cloneRequest function
func TestCloneRequest(t *testing.T) {
	// Test with no body (GET request)
	req := httptest.NewRequest("GET", "http://example.com", nil)
	req.Header.Set("X-Test", "value")
	cloned := cloneRequest(req)

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
	clonedWithBody := cloneRequest(reqWithBody)

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

// TestGenerateCert tests the generateCert method
func TestGenerateCert(t *testing.T) {
	_, rootKey, certPEM, _, err := certs.GenerateRootCA() // Discard rootCA with _
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

	p := NewProxy(parsedRootCA, rootKey)

	// Test generating a new certificate
	cert1, err := p.generateCert("test.com")
	if err != nil {
		t.Fatalf("Failed to generate certificate: %v", err)
	}
	if cert1 == nil {
		t.Error("Generated certificate is nil")
	}

	// Test caching
	cert2, err := p.generateCert("test.com")
	if err != nil {
		t.Fatalf("Failed to retrieve cached certificate: %v", err)
	}
	if cert1 != cert2 {
		t.Errorf("Expected cached certificate to be the same, got different pointers: %p vs %p", cert1, cert2)
	}

	// Test concurrent access
	var wg sync.WaitGroup
	numGoroutines := 10
	wg.Add(numGoroutines)
	certs := make([]*tls.Certificate, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			cert, err := p.generateCert("test.com")
			if err != nil {
				t.Errorf("Goroutine %d failed: %v", idx, err)
			}
			certs[idx] = cert
		}(i)
	}
	wg.Wait()

	for i := 1; i < numGoroutines; i++ {
		if certs[0] != certs[i] {
			t.Errorf("Expected all certificates to be the same (cached), got different at index %d: %p vs %p", i, certs[0], certs[i])
		}
	}
}

func TestServeHTTPOutOfScope(t *testing.T) {
	_, rootKey, _, _, err := certs.GenerateRootCA()
	if err != nil {
		t.Fatalf("Failed to generate Root CA: %v", err)
	}

	p := NewProxy(nil, rootKey)
	p.InScopeFunc = func(req *http.Request) bool {
		return false // Out of scope
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
}
