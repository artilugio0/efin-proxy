package proxy

import (
	"bytes"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"

	"github.com/artilugio0/proxy-vibes/internal/certs"
)

// ModifyFunc defines the signature for request modification function
type ModifyFunc func(*http.Request) *http.Request

// Proxy struct holds the proxy configuration
type Proxy struct {
	ModifyRequest ModifyFunc
	Client        *http.Client
	CertCache     map[string]*tls.Certificate
	CertMutex     sync.RWMutex
	RootCA        *x509.Certificate
	RootKey       *rsa.PrivateKey
}

// NewProxy creates a new proxy instance with a Root CA
func NewProxy(rootCA *x509.Certificate, rootKey *rsa.PrivateKey) *Proxy {
	return &Proxy{
		ModifyRequest: defaultModifyRequest,
		Client: &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true, // For testing; remove in production
				},
			},
		},
		CertCache: make(map[string]*tls.Certificate),
		CertMutex: sync.RWMutex{},
		RootCA:    rootCA,
		RootKey:   rootKey,
	}
}

// defaultModifyRequest returns the request unchanged
func defaultModifyRequest(req *http.Request) *http.Request {
	return req
}

// ServeHTTP handles incoming HTTP requests
func (p *Proxy) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	modifiedReq := p.ModifyRequest(cloneRequest(req))
	modifiedReq.RequestURI = "" // Clear RequestURI for client-side request

	log.Printf("Original request: %s %s", req.Method, req.URL)
	log.Printf("Modified request: %s %s", modifiedReq.Method, modifiedReq.URL)

	resp, err := p.Client.Do(modifiedReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error forwarding request: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
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
		bodyBytes, err := io.ReadAll(req.Body)
		if err != nil {
			log.Printf("Error reading request body: %v", err)
		}
		req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		r.ContentLength = req.ContentLength
	} else {
		r.Body = nil // Explicitly set to nil if no body
	}

	return r
}

// generateCert generates a certificate for a given host, caching it
func (p *Proxy) generateCert(host string) (*tls.Certificate, error) {
	p.CertMutex.RLock()
	if cert, ok := p.CertCache[host]; ok {
		p.CertMutex.RUnlock()
		return cert, nil
	}
	p.CertMutex.RUnlock()

	cert, err := certs.GenerateCert([]string{host}, p.RootCA, p.RootKey)
	if err != nil {
		return nil, err
	}

	p.CertMutex.Lock()
	p.CertCache[host] = cert
	p.CertMutex.Unlock()

	return cert, nil
}
