package hooks

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/artilugio0/efin-proxy/internal/ids"
	"github.com/artilugio0/efin-proxy/internal/pipeline"
)

// RawRequestBytes generates the raw HTTP bytes for a request
func RawRequestBytes(req *http.Request) ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteString(fmt.Sprintf("%s %s %s\r\n", req.Method, req.URL.RequestURI(), req.Proto))

	host := req.Host
	if host == "" {
		host = req.Header.Get("Host")
	}
	if host != "" && req.Header.Get("Host") == "" {
		req.Header.Set("Host", host)
	}

	for key, values := range req.Header {
		for _, value := range values {
			buf.WriteString(fmt.Sprintf("%s: %s\r\n", key, value))
		}
	}

	buf.WriteString("\r\n")

	if req.Body != nil {
		bodyBytes, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %v", err)
		}
		req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		buf.Write(bodyBytes)
	}

	return buf.Bytes(), nil
}

// RawResponseBytes generates the raw HTTP bytes for a response
func RawResponseBytes(resp *http.Response) ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteString(fmt.Sprintf("%s %d %s\r\n", resp.Proto, resp.StatusCode, http.StatusText(resp.StatusCode)))

	for key, values := range resp.Header {
		for _, value := range values {
			buf.WriteString(fmt.Sprintf("%s: %s\r\n", key, value))
		}
	}

	buf.WriteString("\r\n")

	if resp.Body != nil {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %v", err)
		}
		resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		buf.Write(bodyBytes)
	}

	return buf.Bytes(), nil
}

// LogRawRequest prints the request in raw HTTP format to stdout with request ID
func LogRawRequest(req *http.Request) error {
	raw, err := RawRequestBytes(req)
	if err != nil {
		return err
	}
	id := ids.GetRequestID(req)
	if id == "" {
		id = "unknown"
	}
	header := fmt.Sprintf("---------- PROXY-VIBES REQUEST START: %s ----------\r\n", id)
	footer := fmt.Sprintf("---------- PROXY-VIBES REQUEST END: %s ----------\r\n", id)
	fmt.Printf("%s%s%s", header, raw, footer)
	return nil
}

// LogRawResponse prints the response in raw HTTP format to stdout with request ID
func LogRawResponse(resp *http.Response) error {
	raw, err := RawResponseBytes(resp)
	if err != nil {
		return err
	}
	id := ids.GetResponseID(resp)
	if id == "" {
		id = "unknown"
	}
	header := fmt.Sprintf("---------- PROXY-VIBES RESPONSE START: %s ----------\r\n", id)
	footer := fmt.Sprintf("---------- PROXY-VIBES RESPONSE END: %s ----------\r\n", id)
	fmt.Printf("%s%s%s", header, raw, footer)
	return nil
}

// NewFileSaveHooks returns request and response hooks that save to files in the specified directory
func NewFileSaveHooks(dir string) (pipeline.ReadOnlyHook[*http.Request], pipeline.ReadOnlyHook[*http.Response]) {
	if dir == "" {
		dir = "." // Default to current directory
	}

	saveRequest := func(req *http.Request) error {
		raw, err := RawRequestBytes(req)
		if err != nil {
			return err
		}
		id := ids.GetRequestID(req)
		if id == "" {
			id = "unknown"
		}
		filename := filepath.Join(dir, fmt.Sprintf("request-%s.txt", id))
		return os.WriteFile(filename, raw, 0644)
	}

	saveResponse := func(resp *http.Response) error {
		raw, err := RawResponseBytes(resp)
		if err != nil {
			return err
		}
		id := ids.GetResponseID(resp)
		if id == "" {
			id = "unknown"
		}
		filename := filepath.Join(dir, fmt.Sprintf("response-%s.txt", id))
		return os.WriteFile(filename, raw, 0644)
	}

	return saveRequest, saveResponse
}
