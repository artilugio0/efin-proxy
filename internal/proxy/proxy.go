package proxy

import (
	"bufio"
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
	"github.com/artilugio0/proxy-vibes/internal/httpbytes"
	"github.com/artilugio0/proxy-vibes/internal/ids"
	"github.com/artilugio0/proxy-vibes/internal/pipeline"
	"github.com/artilugio0/proxy-vibes/internal/websockets"
	"github.com/google/uuid"
)

// InScopeFunc defines the signature for determining if a request is in scope
type InScopeFunc func(*http.Request) bool

// Proxy struct holds the proxy configuration with pipelines and scope function
type Proxy struct {
	requestInPipeline   *pipeline.ReadOnlyPipeline[*http.Request]  // First request pipeline: read-only
	requestModPipeline  *pipeline.ModPipeline[*http.Request]       // Second request pipeline: read/write
	requestOutPipeline  *pipeline.ReadOnlyPipeline[*http.Request]  // Third request pipeline: read-only
	responseInPipeline  *pipeline.ReadOnlyPipeline[*http.Response] // First response pipeline: read-only
	responseModPipeline *pipeline.ModPipeline[*http.Response]      // Second response pipeline: read/write
	responseOutPipeline *pipeline.ReadOnlyPipeline[*http.Response] // Third response pipeline: read-only

	inScopeFuncMutex sync.RWMutex // Function to determine request scope
	inScopeFunc      InScopeFunc  // Function to determine request scope

	Client *http.Client

	CertCache map[string]*tls.Certificate
	CertMutex sync.RWMutex
	RootCA    *x509.Certificate
	RootKey   *rsa.PrivateKey
}

// NewProxy creates a new proxy instance with empty pipelines and default in-scope function
func NewProxy(rootCA *x509.Certificate, rootKey *rsa.PrivateKey) *Proxy {
	p := &Proxy{
		requestInPipeline:   pipeline.NewReadOnlyPipeline[*http.Request](nil),
		requestModPipeline:  pipeline.NewModPipeline[*http.Request](nil),
		requestOutPipeline:  pipeline.NewReadOnlyPipeline[*http.Request](nil),
		responseInPipeline:  pipeline.NewReadOnlyPipeline[*http.Response](nil),
		responseModPipeline: pipeline.NewModPipeline[*http.Response](nil),
		responseOutPipeline: pipeline.NewReadOnlyPipeline[*http.Response](nil),

		inScopeFuncMutex: sync.RWMutex{},
		inScopeFunc:      func(*http.Request) bool { return true }, // Default: all requests in scope

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
	req = ids.SetRequestID(req, id)

	var finalReq *http.Request
	var err error

	var inScope InScopeFunc
	p.inScopeFuncMutex.RLock()
	inScope = p.inScopeFunc
	p.inScopeFuncMutex.RUnlock()

	if inScope(req) {
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

	finalResp := resp
	if inScope(req) {
		finalResp, err = p.processResponsePipelines(resp)
		if err != nil {
			http.Error(w, fmt.Sprintf("Response pipeline error: %v", err), http.StatusInternalServerError)
			return
		}
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
	req = ids.SetRequestID(req, id)

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
			httpReq = ids.SetRequestID(httpReq, reqID)

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

			var inScope InScopeFunc
			p.inScopeFuncMutex.RLock()
			inScope = p.inScopeFunc
			p.inScopeFuncMutex.RUnlock()

			finalReq := httpReq
			if inScope(httpReq) {
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
			if inScope(httpReq) {
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
	currentReq := httpbytes.CloneRequest(req)

	p.requestInPipeline.RunPipeline(currentReq)
	currentReq = httpbytes.CloneRequest(currentReq) // avoid race conditions between running ro hooks and mod hooks

	currentReq, err := p.requestModPipeline.RunPipeline(currentReq)
	if err != nil {
		return nil, err
	}

	p.requestOutPipeline.RunPipeline(currentReq)

	return currentReq, nil
}

// processResponsePipelines processes the response through all three response pipelines
func (p *Proxy) processResponsePipelines(resp *http.Response) (*http.Response, error) {
	currentResp := httpbytes.CloneResponse(resp)

	p.responseInPipeline.RunPipeline(currentResp)
	currentResp = httpbytes.CloneResponse(currentResp) // avoid race conditions between running ro hooks and mod hooks

	currentResp, err := p.responseModPipeline.RunPipeline(currentResp)
	if err != nil {
		return nil, err
	}

	p.responseOutPipeline.RunPipeline(currentResp)

	return currentResp, nil
}

func (p *Proxy) SetRequestInHooks(hooks []pipeline.ReadOnlyHook[*http.Request]) {
	p.requestInPipeline.SetHooks(hooks)
}

func (p *Proxy) SetRequestModHooks(hooks []pipeline.ModHook[*http.Request]) {
	p.requestModPipeline.SetHooks(hooks)
}

func (p *Proxy) SetRequestOutHooks(hooks []pipeline.ReadOnlyHook[*http.Request]) {
	p.requestOutPipeline.SetHooks(hooks)
}

func (p *Proxy) SetResponseInHooks(hooks []pipeline.ReadOnlyHook[*http.Response]) {
	p.responseInPipeline.SetHooks(hooks)
}

func (p *Proxy) SetResponseModHooks(hooks []pipeline.ModHook[*http.Response]) {
	p.responseModPipeline.SetHooks(hooks)
}

func (p *Proxy) SetResponseOutHooks(hooks []pipeline.ReadOnlyHook[*http.Response]) {
	p.responseOutPipeline.SetHooks(hooks)
}

func (p *Proxy) SetScope(scope InScopeFunc) {
	p.inScopeFuncMutex.Lock()
	p.inScopeFunc = scope
	p.inScopeFuncMutex.Unlock()
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

func getRootCAPool(rootCA *x509.Certificate) *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(rootCA)
	return pool
}
