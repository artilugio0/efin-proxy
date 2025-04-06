package proxy

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"

	"github.com/artilugio0/proxy-vibes/internal/websockets"
)

// HandleConnect handles HTTPS CONNECT requests with MITM, request modification, and WebSocket passthrough
func HandleConnect(p *Proxy, w http.ResponseWriter, req *http.Request) {
	// Dial destination using the original host from CONNECT request
	destConn, err := net.Dial("tcp", req.URL.Host)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error connecting to destination: %v", err), http.StatusBadGateway)
		return
	}

	// Hijack client connection
	clientConn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		http.Error(w, fmt.Sprintf("Error hijacking connection: %v", err), http.StatusInternalServerError)
		destConn.Close()
		return
	}

	// Send 200 OK to client
	_, err = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	if err != nil {
		clientConn.Close()
		destConn.Close()
		return
	}

	// Get certificate for host (using original CONNECT host)
	host, _, _ := net.SplitHostPort(req.URL.Host)
	cert, err := p.generateCert(host)
	if err != nil {
		log.Printf("Error generating certificate: %v", err)
		clientConn.Close()
		destConn.Close()
		return
	}

	// Wrap client connection with TLS
	tlsClientConn := tls.Server(clientConn, &tls.Config{
		Certificates: []tls.Certificate{*cert},
	})

	// Handle TLS connection and intercept HTTP requests
	go func() {
		defer tlsClientConn.Close()
		defer destConn.Close() // Close only when goroutine exits

		// Create a buffered reader for the client TLS connection
		clientReader := bufio.NewReader(tlsClientConn)

		// Wrap destination connection with TLS using the original CONNECT host
		tlsDestConn := tls.Client(destConn, &tls.Config{
			InsecureSkipVerify: true, // For testing; configure properly in production
			ServerName:         host, // Use original CONNECT host for TLS handshake
		})
		defer tlsDestConn.Close()

		// Create a buffered reader for the destination TLS connection
		destReader := bufio.NewReader(tlsDestConn)

		for {
			// Read HTTP request from the TLS connection
			httpReq, err := http.ReadRequest(clientReader)
			if err != nil {
				if err != io.EOF {
					log.Printf("Error reading request from TLS connection: %v", err)
				}
				return // Exit on EOF or error (client closed connection)
			}

			if websockets.IsWebSocketRequest(httpReq) {
				// WebSocket passthrough: pipe connections without modification
				log.Printf("WebSocket connection detected for %s, passing through", httpReq.URL)

				// Write original request to destination
				err = httpReq.Write(tlsDestConn)
				if err != nil {
					log.Printf("Error writing WebSocket request to destination: %v", err)
					return
				}

				// Pipe data bidirectionally
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
				return // Exit after WebSocket connection closes
			} else {
				// Regular HTTP request: modify and forward
				modifiedReq := p.ModifyRequest(httpReq)
				log.Printf("Original CONNECT request: %s %s", httpReq.Method, httpReq.URL)
				log.Printf("Modified CONNECT request: %s %s", modifiedReq.Method, modifiedReq.URL)

				// Write modified request to destination using the same TLS connection
				err = modifiedReq.Write(tlsDestConn)
				if err != nil {
					log.Printf("Error writing modified request to destination: %v", err)
					return
				}

				// Read response from destination
				resp, err := http.ReadResponse(destReader, modifiedReq)
				if err != nil {
					log.Printf("Error reading response from destination: %v", err)
					return
				}
				defer resp.Body.Close()

				// Write response back to client
				err = resp.Write(tlsClientConn)
				if err != nil {
					log.Printf("Error writing response to client: %v", err)
					return
				}
			}
		}
	}()
}
