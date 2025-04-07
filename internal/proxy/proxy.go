package proxy

import (
	"bufio"
	"bytes"
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

// RequestInOutFunc defines the signature for read-only request pipeline functions
type RequestInOutFunc func(*http.Request) error

// RequestModFunc defines the signature for read/write request pipeline functions
type RequestModFunc func(*http.Request) (*http.Request, error)

// ResponseInOutFunc defines the signature for read-only response pipeline functions
type ResponseInOutFunc func(*http.Response) error

// ResponseModFunc defines the signature for read/write response pipeline functions
type ResponseModFunc func(*http.Response) (*http.Response, error)

// InScopeFunc defines the signature for determining if a request is in scope
type InScopeFunc func(*http.Request) bool

type requestIDKeyType struct{}

var requestIDKey = requestIDKeyType{}

// Proxy struct holds the proxy configuration with pipelines and scope function
type Proxy struct {
	RequestInPipeline   []RequestInOutFunc  // First request pipeline: read-only
	RequestModPipeline  []RequestModFunc    // Second request pipeline: read/write
	RequestOutPipeline  []RequestInOutFunc  // Third request pipeline: read-only
	ResponseInPipeline  []ResponseInOutFunc // First response pipeline: read-only
	ResponseModPipeline []ResponseModFunc   // Second response pipeline: read/write
	ResponseOutPipeline []ResponseInOutFunc // Third response pipeline: read-only
	InScopeFunc         InScopeFunc         // Function to determine request scope
	Client              *http.Client
	CertCache           map[string]*tls.Certificate
	CertMutex           sync.RWMutex
	RootCA              *x509.Certificate
	RootKey             *rsa.PrivateKey
}

// NewProxy creates a new proxy instance with empty pipelines and default in-scope function
func NewProxy(rootCA *x509.Certificate, rootKey *rsa.PrivateKey) *Proxy {
	return &Proxy{
		RequestInPipeline:   []RequestInOutFunc{},
		RequestModPipeline:  []RequestModFunc{},
		RequestOutPipeline:  []RequestInOutFunc{},
		ResponseInPipeline:  []ResponseInOutFunc{},
		ResponseModPipeline: []ResponseModFunc{},
		ResponseOutPipeline: []ResponseInOutFunc{},
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
		log.Printf("Request out of scope: %s %s", req.Method, req.URL)
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

			var finalReq *http.Request
			if p.InScopeFunc(httpReq) {
				finalReq, err = p.processRequestPipelines(httpReq)
				if err != nil {
					log.Printf("Request pipeline error: %v", err)
					return
				}
			} else {
				finalReq = httpReq
				log.Printf("Request out of scope: %s %s", httpReq.Method, httpReq.URL)
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

			var finalResp *http.Response
			if p.InScopeFunc(httpReq) {
				finalResp, err = p.processResponsePipelines(resp)
				if err != nil {
					log.Printf("Response pipeline error: %v", err)
					return
				}
			} else {
				finalResp = cloneResponse(resp)
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
	//currentReq.Header.Del("accept-encoding")

	if len(p.RequestInPipeline) > 0 {
		var wg sync.WaitGroup
		errChan := make(chan error, len(p.RequestInPipeline))

		for _, fn := range p.RequestInPipeline {
			wg.Add(1)
			go func(f RequestInOutFunc) {
				defer wg.Done()
				tempReq := cloneRequest(currentReq)
				if err := f(tempReq); err != nil {
					errChan <- err
				}
			}(fn)
		}

		wg.Wait()
		close(errChan)

		for err := range errChan {
			if err != nil {
				return nil, fmt.Errorf("RequestInPipeline error: %v", err)
			}
		}
	}

	for _, fn := range p.RequestModPipeline {
		modifiedReq, err := fn(currentReq)
		if err != nil {
			return nil, fmt.Errorf("RequestModPipeline error: %v", err)
		}
		currentReq = modifiedReq
		currentReq.Body.(*BodyWrapper).Reset()
	}

	if len(p.RequestOutPipeline) > 0 {
		var wg sync.WaitGroup
		errChan := make(chan error, len(p.RequestOutPipeline))

		for _, fn := range p.RequestOutPipeline {
			wg.Add(1)
			go func(f RequestInOutFunc) {
				defer wg.Done()
				tempReq := cloneRequest(currentReq)
				if err := f(tempReq); err != nil {
					errChan <- err
				}
			}(fn)
		}

		wg.Wait()
		close(errChan)

		for err := range errChan {
			if err != nil {
				return nil, fmt.Errorf("RequestOutPipeline error: %v", err)
			}
		}
	}

	return currentReq, nil
}

// processResponsePipelines processes the response through all three response pipelines
func (p *Proxy) processResponsePipelines(resp *http.Response) (*http.Response, error) {
	currentResp := cloneResponse(resp)

	if len(p.ResponseInPipeline) > 0 {
		var wg sync.WaitGroup
		errChan := make(chan error, len(p.ResponseInPipeline))

		for _, fn := range p.ResponseInPipeline {
			wg.Add(1)
			go func(f ResponseInOutFunc) {
				defer wg.Done()
				tempResp := cloneResponse(currentResp)
				if err := f(tempResp); err != nil {
					errChan <- err
				}
			}(fn)
		}

		wg.Wait()
		close(errChan)

		for err := range errChan {
			if err != nil {
				return nil, fmt.Errorf("ResponseInPipeline error: %v", err)
			}
		}
	}

	for _, fn := range p.ResponseModPipeline {
		modifiedResp, err := fn(currentResp)
		if err != nil {
			return nil, fmt.Errorf("ResponseModPipeline error: %v", err)
		}
		currentResp = modifiedResp
		currentResp.Body.(*BodyWrapper).Reset()
	}

	if len(p.ResponseOutPipeline) > 0 {
		var wg sync.WaitGroup
		errChan := make(chan error, len(p.ResponseOutPipeline))

		for _, fn := range p.ResponseOutPipeline {
			wg.Add(1)
			go func(f ResponseInOutFunc) {
				defer wg.Done()
				tempResp := cloneResponse(currentResp)
				if err := f(tempResp); err != nil {
					errChan <- err
				}
			}(fn)
		}

		wg.Wait()
		close(errChan)

		for err := range errChan {
			if err != nil {
				return nil, fmt.Errorf("ResponseOutPipeline error: %v", err)
			}
		}
	}

	return currentResp, nil
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
		if wrapper, ok := req.Body.(*BodyWrapper); ok {
			r.Body = wrapper.ShallowClone()
		} else {
			bodyBytes, err := io.ReadAll(req.Body)
			if err != nil {
				log.Printf("Error reading request body: %v", err)
			}
			newBody := NewBodyWrapper(bodyBytes)
			req.Body = newBody
			r.Body = newBody.ShallowClone()
			r.ContentLength = req.ContentLength
		}
	} else {
		r.Body = nil
	}

	return r
}

// cloneResponse creates a deep copy of an HTTP response
func cloneResponse(resp *http.Response) *http.Response {
	r := new(http.Response)
	*r = *resp

	r.Header = make(http.Header)
	for k, v := range resp.Header {
		r.Header[k] = append([]string(nil), v...)
	}

	if resp.Body != nil {
		if wrapper, ok := resp.Body.(*BodyWrapper); ok {
			r.Body = wrapper.ShallowClone()
		} else {
			bodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				log.Printf("Error reading response body: %v", err)
			}
			newBody := NewBodyWrapper(bodyBytes)
			resp.Body = newBody
			r.Body = newBody.ShallowClone()
		}
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

// GetRequestID retrieves the request ID from the request's context
func GetRequestID(req *http.Request) string {
	if id, ok := req.Context().Value(requestIDKey).(string); ok {
		return id
	}
	return "" // Return empty string if no ID found
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

// BodyWrapper is a type that wraps a byte array and implements io.ReadCloser
type BodyWrapper struct {
	data   []byte        // The underlying byte array
	reader *bytes.Reader // The reader for the byte array
}

// NewBodyWrapper creates a new BodyWrapper from a byte slice
func NewBodyWrapper(data []byte) *BodyWrapper {
	return &BodyWrapper{
		data:   data,
		reader: bytes.NewReader(data),
	}
}

// Read implements the io.Reader interface
func (bw *BodyWrapper) Read(p []byte) (n int, err error) {
	return bw.reader.Read(p)
}

// Close implements the io.Closer interface (no-op in this case)
func (bw *BodyWrapper) Close() error {
	// Since we're using bytes.Reader, there's nothing to close,
	// but we implement this for io.ReadCloser compatibility
	return nil
}

// ShallowClone creates a new BodyWrapper instance with the same underlying
// byte array and a fresh reader reset to the start
func (bw *BodyWrapper) ShallowClone() *BodyWrapper {
	return &BodyWrapper{
		data:   bw.data,                  // Reference the same byte array (shallow copy)
		reader: bytes.NewReader(bw.data), // New reader starting at position 0
	}
}

// Reset resets the reader's position to the beginning of the byte array
func (bw *BodyWrapper) Reset() {
	bw.reader.Seek(0, io.SeekStart)
}
