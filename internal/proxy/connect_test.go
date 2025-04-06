package proxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/artilugio0/proxy-vibes/internal/certs"
	"github.com/gorilla/websocket"
)

// TestHandleConnectRegularHTTPRequest tests handling of a regular HTTP request over CONNECT
func TestHandleConnectRegularHTTPRequest(t *testing.T) {
	// Generate Root CA
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

	// Create proxy
	p := NewProxy(rootCA, rootKey)
	p.ModifyRequest = func(req *http.Request) *http.Request {
		req.Header.Set("X-Modified", "true")
		return req
	}

	// Create a real HTTPS test server as the destination
	serverCert, err := certs.GenerateCert([]string{"localhost", "127.0.0.1"}, rootCA, rootKey)
	if err != nil {
		t.Fatalf("Failed to generate server certificate: %v", err)
	}
	destServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Echo back the X-Modified header from the request
		if val := r.Header.Get("X-Modified"); val != "" {
			w.Header().Set("X-Modified", val)
		}
		w.Write([]byte("Hello, World!"))
	}))
	destServer.TLS.Certificates = []tls.Certificate{*serverCert}
	defer destServer.Close()

	// Extract destination server address
	destAddr := destServer.Listener.Addr().String()

	// Create a proxy server to handle CONNECT
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleConnect(p, w, r)
	}))
	defer proxyServer.Close()

	// Create an HTTP client with the proxy
	proxyURL, _ := url.Parse("http://" + proxyServer.Listener.Addr().String())
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{
				RootCAs: getRootCAPool(parsedRootCA),
			},
		},
	}

	// Send a CONNECT request followed by an HTTPS request to the local test server
	resp, err := client.Get("https://localhost:" + destAddr[strings.LastIndex(destAddr, ":")+1:])
	if err != nil {
		t.Fatalf("Failed to perform request through proxy: %v", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}
	response := string(body)

	// Verify response and modification
	if !strings.Contains(response, "Hello, World!") {
		t.Errorf("Expected response to contain 'Hello, World!', got %s", response)
	}
	if resp.Header.Get("X-Modified") != "true" {
		t.Errorf("Expected X-Modified header to be 'true', got %s", resp.Header.Get("X-Modified"))
	}
}

// TestHandleConnectWebSocketRequest tests handling of a WebSocket request over CONNECT
func TestHandleConnectWebSocketRequest(t *testing.T) {
	// Generate Root CA
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

	// Create proxy
	p := NewProxy(rootCA, rootKey)
	p.ModifyRequest = func(req *http.Request) *http.Request {
		req.Header.Set("X-Modified", "true")
		return req
	}

	// Create a WebSocket test server as the destination
	serverCert, err := certs.GenerateCert([]string{"localhost", "127.0.0.1"}, rootCA, rootKey)
	if err != nil {
		t.Fatalf("Failed to generate server certificate: %v", err)
	}
	var upgrader = websocket.Upgrader{}
	destServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatal(err)
			return
		}
		defer conn.Close()
		conn.WriteMessage(websocket.TextMessage, []byte("Hello WebSocket!"))
	}))
	destServer.TLS.Certificates = []tls.Certificate{*serverCert}
	defer destServer.Close()

	// Extract destination server address
	destAddr := destServer.Listener.Addr().String()

	// Create a proxy server to handle CONNECT
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleConnect(p, w, r)
	}))
	defer proxyServer.Close()

	// Create a WebSocket client with the proxy
	proxyURL, _ := url.Parse("http://" + proxyServer.Listener.Addr().String())
	dialer := &websocket.Dialer{
		Proxy: http.ProxyURL(proxyURL),
		TLSClientConfig: &tls.Config{
			RootCAs: getRootCAPool(parsedRootCA),
		},
	}
	wsURL := "wss://localhost:" + destAddr[strings.LastIndex(destAddr, ":")+1:]
	conn, resp, err := dialer.Dial(wsURL, http.Header{
		"Host": []string{"localhost"},
	})
	if err != nil {
		t.Fatalf("Failed to establish WebSocket connection: %v", err)
	}
	defer conn.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("Expected status 101 Switching Protocols, got %d", resp.StatusCode)
	}

	// Read WebSocket message
	_, message, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read WebSocket message: %v", err)
	}

	// Verify WebSocket passthrough (no modification)
	response := string(message)
	if !strings.Contains(response, "Hello WebSocket!") {
		t.Errorf("Expected WebSocket response to contain 'Hello WebSocket!', got %s", response)
	}
	if resp.Header.Get("X-Modified") == "true" {
		t.Errorf("WebSocket request should not be modified, but found X-Modified header")
	}
}

// TestHandleConnectInvalidDestination tests handling of an invalid destination
func TestHandleConnectInvalidDestination(t *testing.T) {
	// Generate Root CA
	rootCA, rootKey, _, _, err := certs.GenerateRootCA()
	if err != nil {
		t.Fatalf("Failed to generate Root CA: %v", err)
	}

	// Create proxy with a mock dialer that fails
	p := NewProxy(rootCA, rootKey)
	p.Client.Transport.(*http.Transport).DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return nil, net.ErrClosed
	}

	// Simulate CONNECT request to an invalid host
	connectReq := httptest.NewRequest(http.MethodConnect, "https://nonexistent.invalid:443", nil)
	writer := httptest.NewRecorder()

	// Run HandleConnect
	HandleConnect(p, writer, connectReq)

	// Verify error response
	resp := writer.Result()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("Expected status %d, got %d", http.StatusBadGateway, resp.StatusCode)
	}
}

// getRootCAPool creates a CertPool with the given Root CA
func getRootCAPool(rootCA *x509.Certificate) *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(rootCA)
	return pool
}
