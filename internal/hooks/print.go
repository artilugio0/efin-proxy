package hooks

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/artilugio0/proxy-vibes/internal/proxy" // Import proxy for GetRequestID and GetResponseID
)

// rawRequestString generates the raw HTTP string for a request
func rawRequestString(req *http.Request) (string, error) {
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
			return "", fmt.Errorf("failed to read request body: %v", err)
		}
		req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		buf.Write(bodyBytes)
	}

	return buf.String(), nil
}

// rawResponseString generates the raw HTTP string for a response
func rawResponseString(resp *http.Response) (string, error) {
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
			return "", fmt.Errorf("failed to read response body: %v", err)
		}
		resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		buf.Write(bodyBytes)
	}

	return buf.String(), nil
}

// LogRawRequest prints the request in raw HTTP format to stdout with request ID
func LogRawRequest(req *http.Request) error {
	raw, err := rawRequestString(req)
	if err != nil {
		return err
	}
	if id := proxy.GetRequestID(req); id != "" {
		fmt.Printf("Request ID: %s\n%s", id, raw)
	} else {
		fmt.Println(raw)
	}
	return nil
}

// LogRawResponse prints the response in raw HTTP format to stdout with request ID
func LogRawResponse(resp *http.Response) error {
	raw, err := rawResponseString(resp)
	if err != nil {
		return err
	}
	if id := proxy.GetResponseID(resp); id != "" {
		fmt.Printf("Response ID: %s\n%s", id, raw)
	} else {
		fmt.Println(raw)
	}
	return nil
}
