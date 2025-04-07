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

func TestHandleConnectRegularHTTPRequest(t *testing.T) {
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

	p := NewProxy(rootCA, rootKey)
	p.RequestModPipeline = append(p.RequestModPipeline, func(req *http.Request) (*http.Request, error) {
		req.Header.Set("X-Modified", "true")
		return req, nil
	})

	serverCert, err := certs.GenerateCert([]string{"localhost", "127.0.0.1"}, rootCA, rootKey)
	if err != nil {
		t.Fatalf("Failed to generate server certificate: %v", err)
	}
	destServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if val := r.Header.Get("X-Modified"); val != "" {
			w.Header().Set("X-Modified", val)
		}
		w.Write([]byte("Hello, World!"))
	}))
	destServer.TLS.Certificates = []tls.Certificate{*serverCert}
	defer destServer.Close()

	destAddr := destServer.Listener.Addr().String()

	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.HandleConnect(w, r) // Changed to method call
	}))
	defer proxyServer.Close()

	proxyURL, _ := url.Parse("http://" + proxyServer.Listener.Addr().String())
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{
				RootCAs: getRootCAPool(parsedRootCA),
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

	if !strings.Contains(response, "Hello, World!") {
		t.Errorf("Expected response to contain 'Hello, World!', got %s", response)
	}
	if resp.Header.Get("X-Modified") != "true" {
		t.Errorf("Expected X-Modified header to be 'true', got %s", resp.Header.Get("X-Modified"))
	}
}

func TestHandleConnectWebSocketRequest(t *testing.T) {
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

	p := NewProxy(rootCA, rootKey)
	p.RequestModPipeline = append(p.RequestModPipeline, func(req *http.Request) (*http.Request, error) {
		req.Header.Set("X-Modified", "true")
		return req, nil
	})

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

	destAddr := destServer.Listener.Addr().String()

	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.HandleConnect(w, r) // Changed to method call
	}))
	defer proxyServer.Close()

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

	_, message, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read WebSocket message: %v", err)
	}

	response := string(message)
	if !strings.Contains(response, "Hello WebSocket!") {
		t.Errorf("Expected WebSocket response to contain 'Hello WebSocket!', got %s", response)
	}
	if resp.Header.Get("X-Modified") == "true" {
		t.Errorf("WebSocket request should not be modified, but found X-Modified header")
	}
}

func TestHandleConnectInvalidDestination(t *testing.T) {
	rootCA, rootKey, _, _, err := certs.GenerateRootCA()
	if err != nil {
		t.Fatalf("Failed to generate Root CA: %v", err)
	}

	p := NewProxy(rootCA, rootKey)
	p.Client.Transport.(*http.Transport).DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return nil, net.ErrClosed
	}

	connectReq := httptest.NewRequest(http.MethodConnect, "https://nonexistent.invalid:443", nil)
	writer := httptest.NewRecorder()

	p.HandleConnect(writer, connectReq) // Changed to method call

	resp := writer.Result()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("Expected status %d, got %d", http.StatusBadGateway, resp.StatusCode)
	}
}
