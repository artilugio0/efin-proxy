package proxy

import (
	"bufio"
	"context"
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
	"github.com/google/uuid"
)

// InScopeFunc defines the signature for determining if a request is in scope
type InScopeFunc func(*http.Request) bool

type requestIDKeyType struct{}

var requestIDKey = requestIDKeyType{}

// Proxy struct holds the proxy configuration with pipelines and scope function
type Proxy struct {
	RequestInPipeline   *readOnlyPipeline[*http.Request]  // First request pipeline: read-only
	RequestModPipeline  *modPipeline[*http.Request]       // Second request pipeline: read/write
	RequestOutPipeline  *readOnlyPipeline[*http.Request]  // Third request pipeline: read-only
	ResponseInPipeline  *readOnlyPipeline[*http.Response] // First response pipeline: read-only
	ResponseModPipeline *modPipeline[*http.Response]      // Second response pipeline: read/write
	ResponseOutPipeline *readOnlyPipeline[*http.Response] // Third response pipeline: read-only
	InScopeFunc         InScopeFunc                       // Function to determine request scope
	Client              *http.Client
	CertCache           map[string]*tls.Certificate
	CertMutex           sync.RWMutex
	RootCA              *x509.Certificate
	RootKey             *rsa.PrivateKey
}

// NewProxy creates a new proxy instance with empty pipelines and default in-scope function
func NewProxy(rootCA *x509.Certificate, rootKey *rsa.PrivateKey) *Proxy {
	p := &Proxy{
		RequestInPipeline:   newReadOnlyPipeline[*http.Request](nil),
		RequestModPipeline:  newModPipeline[*http.Request](nil),
		RequestOutPipeline:  newReadOnlyPipeline[*http.Request](nil),
		ResponseInPipeline:  newReadOnlyPipeline[*http.Response](nil),
		ResponseModPipeline: newModPipeline[*http.Response](nil),
		ResponseOutPipeline: newReadOnlyPipeline[*http.Response](nil),
		InScopeFunc:         func(*http.Request) bool { return true }, // Default: all requests in scope
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

	return p
}

// ServeHTTP handles incoming HTTP requests and responses with scope checking
func (p *Proxy) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Generate UUID v4 and add to request context
	id := uuid.New().String() // UUID v4
	ctx := context.WithValue(req.Context(), requestIDKey, id)
	req = req.WithContext(ctx)

	var finalReq *http.Request
	var err error

	if p.InScopeFunc(req) {
		finalReq, err = p.processRequestPipelines(req)
		if err != nil {
			http.Error(w, fmt.Sprintf("Request pipeline error: %v", err), http.StatusInternalServerError)
			return
		}
		log.Printf("Original request: %s %s", req.Method, req.URL)
		log.Printf("Final request: %s %s", finalReq.Method, finalReq.URL)
	} else {
		finalReq = req
	}

	finalReq.RequestURI = ""

	resp, err := p.Client.Do(finalReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error forwarding request: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	var finalResp *http.Response
	if p.InScopeFunc(req) {
		finalResp, err = p.processResponsePipelines(resp)
		if err != nil {
			http.Error(w, fmt.Sprintf("Response pipeline error: %v", err), http.StatusInternalServerError)
			return
		}
	} else {
		finalResp = cloneResponse(resp)
	}

	for key, values := range finalResp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(finalResp.StatusCode)
	io.Copy(w, finalResp.Body)
	finalResp.Body.Close()
}

// HandleConnect handles HTTPS CONNECT requests with MITM and pipeline processing
func (p *Proxy) HandleConnect(w http.ResponseWriter, req *http.Request) {
	// Generate UUID v4 for the initial CONNECT request (optional, for tracking the tunnel itself)
	id := uuid.New().String()
	ctx := context.WithValue(req.Context(), requestIDKey, id)
	req = req.WithContext(ctx)

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

			// Generate a new UUID v4 for each tunneled request
			reqID := uuid.New().String()
			httpReq = httpReq.WithContext(context.WithValue(httpReq.Context(), requestIDKey, reqID))

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

			finalReq := httpReq
			if p.InScopeFunc(httpReq) {
				finalReq, err = p.processRequestPipelines(httpReq)
				if err != nil {
					log.Printf("Request pipeline error: %v", err)
					return
				}
			}

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

			finalResp := resp
			if p.InScopeFunc(httpReq) {
				finalResp, err = p.processResponsePipelines(resp)
				if err != nil {
					log.Printf("Response pipeline error: %v", err)
					return
				}
			}

			err = finalResp.Write(tlsClientConn)
			if err != nil {
				log.Printf("Error writing response to client: %v", err)
				return
			}
			finalResp.Body.Close()
		}
	}()
}

// processRequestPipelines processes the request through all three request pipelines
func (p *Proxy) processRequestPipelines(req *http.Request) (*http.Request, error) {
	currentReq := cloneRequest(req)

	p.RequestInPipeline.runPipeline(currentReq)

	currentReq, err := p.RequestModPipeline.runPipeline(currentReq)
	if err != nil {
		return nil, err
	}

	p.RequestOutPipeline.runPipeline(currentReq)

	return currentReq, nil
}

// processResponsePipelines processes the response through all three response pipelines
func (p *Proxy) processResponsePipelines(resp *http.Response) (*http.Response, error) {
	currentResp := cloneResponse(resp)

	p.ResponseInPipeline.runPipeline(currentResp)

	currentResp, err := p.ResponseModPipeline.runPipeline(currentResp)
	if err != nil {
		return nil, err
	}

	p.ResponseOutPipeline.runPipeline(currentResp)

	return currentResp, nil
}

func (p *Proxy) SetRequestInHooks(hooks []ReadOnlyHook[*http.Request]) {
	p.RequestInPipeline.setHooks(hooks)
}

func (p *Proxy) SetRequestModHooks(hooks []ModHook[*http.Request]) {
	p.RequestModPipeline.setHooks(hooks)
}

func (p *Proxy) SetRequestOutHooks(hooks []ReadOnlyHook[*http.Request]) {
	p.RequestOutPipeline.setHooks(hooks)
}

func (p *Proxy) SetResponseInHooks(hooks []ReadOnlyHook[*http.Response]) {
	p.ResponseInPipeline.setHooks(hooks)
}

func (p *Proxy) SetResponseModHooks(hooks []ModHook[*http.Response]) {
	p.ResponseModPipeline.setHooks(hooks)
}

func (p *Proxy) SetResponseOutHooks(hooks []ReadOnlyHook[*http.Response]) {
	p.ResponseOutPipeline.setHooks(hooks)
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

// GetRequestID retrieves the request ID from the request's context
func GetRequestID(req *http.Request) string {
	if id, ok := req.Context().Value(requestIDKey).(string); ok {
		return id
	}
	return "" // Return empty string if no ID found
}

func SetRequestID(req *http.Request, id string) *http.Request {
	ctx := context.WithValue(req.Context(), requestIDKey, id)
	return req.WithContext(ctx)
}

// GetResponseID retrieves the request ID from the response's request context
func GetResponseID(resp *http.Response) string {
	if resp.Request != nil {
		if id, ok := resp.Request.Context().Value(requestIDKey).(string); ok {
			return id
		}
	}
	return "" // Return empty string if no ID found or no request
}

func getRootCAPool(rootCA *x509.Certificate) *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(rootCA)
	return pool
}
