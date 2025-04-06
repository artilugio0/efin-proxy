package proxy

import (
	"bufio"
	"bytes"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"

	"github.com/artilugio0/proxy-vibes/internal/certs"
	"github.com/artilugio0/proxy-vibes/internal/websockets"
)

// RequestInOutFunc defines the signature for read-only pipeline functions
type RequestInOutFunc func(*http.Request) error

// RequestModFunc defines the signature for read/write pipeline functions
type RequestModFunc func(*http.Request) (*http.Request, error)

// Proxy struct holds the proxy configuration with three pipelines
type Proxy struct {
	RequestInPipeline  []RequestInOutFunc // First pipeline: read-only
	RequestModPipeline []RequestModFunc   // Second pipeline: read/write
	RequestOutPipeline []RequestInOutFunc // Third pipeline: read-only
	Client             *http.Client
	CertCache          map[string]*tls.Certificate
	CertMutex          sync.RWMutex
	RootCA             *x509.Certificate
	RootKey            *rsa.PrivateKey
}

// NewProxy creates a new proxy instance with empty pipelines
func NewProxy(rootCA *x509.Certificate, rootKey *rsa.PrivateKey) *Proxy {
	return &Proxy{
		RequestInPipeline:  []RequestInOutFunc{},
		RequestModPipeline: []RequestModFunc{},
		RequestOutPipeline: []RequestInOutFunc{},
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

// ServeHTTP handles incoming HTTP requests through the pipelines
func (p *Proxy) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	finalReq, err := p.processRequestPipelines(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("Pipeline error: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Original request: %s %s", req.Method, req.URL)
	log.Printf("Final request: %s %s", finalReq.Method, finalReq.URL)

	finalReq.RequestURI = ""

	resp, err := p.Client.Do(finalReq)
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

// HandleConnect handles HTTPS CONNECT requests with MITM and pipeline processing
func (p *Proxy) HandleConnect(w http.ResponseWriter, req *http.Request) {
	destConn, err := net.Dial("tcp", req.URL.Host)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error connecting to destination: %v", err), http.StatusBadGateway)
		return
	}

	clientConn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		http.Error(w, fmt.Sprintf("Error hijacking connection: %v", err), http.StatusInternalServerError)
		destConn.Close()
		return
	}

	_, err = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	if err != nil {
		clientConn.Close()
		destConn.Close()
		return
	}

	host, _, _ := net.SplitHostPort(req.URL.Host)
	cert, err := p.generateCert(host)
	if err != nil {
		log.Printf("Error generating certificate: %v", err)
		clientConn.Close()
		destConn.Close()
		return
	}

	tlsClientConn := tls.Server(clientConn, &tls.Config{
		Certificates: []tls.Certificate{*cert},
	})

	go func() {
		defer tlsClientConn.Close()
		defer destConn.Close()

		clientReader := bufio.NewReader(tlsClientConn)
		tlsDestConn := tls.Client(destConn, &tls.Config{
			InsecureSkipVerify: true,
			ServerName:         host,
		})
		defer tlsDestConn.Close()
		destReader := bufio.NewReader(tlsDestConn)

		for {
			httpReq, err := http.ReadRequest(clientReader)
			if err != nil {
				if err != io.EOF {
					log.Printf("Error reading request from TLS connection: %v", err)
				}
				return
			}

			if websockets.IsWebSocketRequest(httpReq) {
				log.Printf("WebSocket connection detected for %s, passing through", httpReq.URL)
				err = httpReq.Write(tlsDestConn)
				if err != nil {
					log.Printf("Error writing WebSocket request to destination: %v", err)
					return
				}

				var wg sync.WaitGroup
				wg.Add(2)
				go func() {
					defer wg.Done()
					io.Copy(tlsDestConn, tlsClientConn)
				}()
				go func() {
					defer wg.Done()
					io.Copy(tlsClientConn, tlsDestConn)
				}()
				wg.Wait()
				return
			}

			finalReq, err := p.processRequestPipelines(httpReq)
			if err != nil {
				log.Printf("Pipeline error: %v", err)
				return
			}

			log.Printf("Original CONNECT request: %s %s", httpReq.Method, httpReq.URL)
			log.Printf("Modified CONNECT request: %s %s", finalReq.Method, finalReq.URL)

			err = finalReq.Write(tlsDestConn)
			if err != nil {
				log.Printf("Error writing modified request to destination: %v", err)
				return
			}

			resp, err := http.ReadResponse(destReader, finalReq)
			if err != nil {
				log.Printf("Error reading response from destination: %v", err)
				return
			}
			defer resp.Body.Close()

			err = resp.Write(tlsClientConn)
			if err != nil {
				log.Printf("Error writing response to client: %v", err)
				return
			}
		}
	}()
}

// processRequestPipelines processes the request through all three pipelines
func (p *Proxy) processRequestPipelines(req *http.Request) (*http.Request, error) {
	currentReq := cloneRequest(req)

	// First pipeline: RequestInPipeline (read-only)
	for _, fn := range p.RequestInPipeline {
		// Clone the request for each function to ensure read-only behavior
		tempReq := cloneRequest(currentReq)
		if err := fn(tempReq); err != nil {
			return nil, fmt.Errorf("RequestInPipeline error: %v", err)
		}
	}

	// Second pipeline: RequestModPipeline (read/write)
	for _, fn := range p.RequestModPipeline {
		modifiedReq, err := fn(currentReq)
		if err != nil {
			return nil, fmt.Errorf("RequestModPipeline error: %v", err)
		}
		currentReq = modifiedReq
	}

	// Third pipeline: RequestOutPipeline (read-only)
	for _, fn := range p.RequestOutPipeline {
		// Clone the request for each function to ensure read-only behavior
		tempReq := cloneRequest(currentReq)
		if err := fn(tempReq); err != nil {
			return nil, fmt.Errorf("RequestOutPipeline error: %v", err)
		}
	}

	return currentReq, nil
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
		r.Body = nil
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
