package hooks

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

// rawRequestString generates the raw HTTP string for a request
func rawRequestString(req *http.Request) (string, error) {
	var buf bytes.Buffer

	// Write request line
	buf.WriteString(fmt.Sprintf("%s %s %s\r\n", req.Method, req.URL.RequestURI(), req.Proto))

	// Ensure Host header is included
	host := req.Host
	if host == "" {
		host = req.Header.Get("Host")
	}
	if host != "" && req.Header.Get("Host") == "" {
		req.Header.Set("Host", host)
	}

	// Write headers
	for key, values := range req.Header {
		for _, value := range values {
			buf.WriteString(fmt.Sprintf("%s: %s\r\n", key, value))
		}
	}

	// Write blank line before body
	buf.WriteString("\r\n")

	// Write body if present
	if req.Body != nil {
		bodyBytes, err := io.ReadAll(req.Body)
		if err != nil {
			return "", fmt.Errorf("failed to read request body: %v", err)
		}
		// Restore body for downstream use
		req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		buf.Write(bodyBytes)
	}

	return buf.String(), nil
}

// rawResponseString generates the raw HTTP string for a response
func rawResponseString(resp *http.Response) (string, error) {
	var buf bytes.Buffer

	// Write status line using StatusCode and StatusText to avoid duplication
	buf.WriteString(fmt.Sprintf("%s %d %s\r\n", resp.Proto, resp.StatusCode, http.StatusText(resp.StatusCode)))

	// Write headers
	for key, values := range resp.Header {
		for _, value := range values {
			buf.WriteString(fmt.Sprintf("%s: %s\r\n", key, value))
		}
	}

	// Write blank line before body
	buf.WriteString("\r\n")

	// Write body if present
	if resp.Body != nil {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("failed to read response body: %v", err)
		}
		// Restore body for downstream use
		resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		buf.Write(bodyBytes)
	}

	return buf.String(), nil
}

// LogRawRequest prints the request in raw HTTP format to stdout
func LogRawRequest(req *http.Request) error {
	raw, err := rawRequestString(req)
	if err != nil {
		return err
	}
	fmt.Println(raw)
	return nil
}

// LogRawResponse prints the response in raw HTTP format to stdout
func LogRawResponse(resp *http.Response) error {
	raw, err := rawResponseString(resp)
	if err != nil {
		return err
	}
	fmt.Println(raw)
	return nil
}
