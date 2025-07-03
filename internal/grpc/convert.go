package grpc

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/artilugio0/efin-proxy/internal/ids"
	pb "github.com/artilugio0/efin-proxy/pkg/grpc/proto"
)

// ToProtoRequest converts an http.Request to a proto HttpRequest.
func ToProtoRequest(req *http.Request) *pb.HttpRequest {
	hostAdded := false
	headers := []*pb.Header{}
	for k, vs := range req.Header {
		for _, v := range vs {
			headers = append(headers, &pb.Header{Name: k, Value: v})
			if strings.ToLower(k) == "host" {
				hostAdded = true
			}
		}
	}
	if !hostAdded {
		headers = append(headers, &pb.Header{Name: "Host", Value: req.Host})
	}

	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewBuffer(body))
	}
	return &pb.HttpRequest{
		Id:      ids.GetRequestID(req),
		Method:  req.Method,
		Url:     req.URL.String(),
		Headers: headers,
		Body:    body,
	}
}

// FromProtoRequest converts a proto HttpRequest to an http.Request, using fields from sourceReq for fields not present in protoReq.
func FromProtoRequest(protoReq *pb.HttpRequest, sourceReq *http.Request) (*http.Request, error) {
	// Parse URL from proto
	parsedURL, err := url.Parse(protoReq.Url)
	if err != nil {
		return nil, err
	}

	// Create a new context, defaulting to sourceReq's context if available, else background
	ctx := context.Background()
	if sourceReq != nil && sourceReq.Context() != nil {
		ctx = sourceReq.Context()
	}

	// Create new request with proto fields
	req, err := http.NewRequestWithContext(
		ctx,
		protoReq.Method,
		protoReq.Url,
		io.NopCloser(bytes.NewReader(protoReq.Body)),
	)
	if err != nil {
		return nil, err
	}

	// Set headers from proto
	hostHeader := ""
	req.Header = make(http.Header)
	for _, h := range protoReq.Headers {
		req.Header.Add(h.Name, h.Value)
		if strings.ToLower(h.Name) == "host" {
			hostHeader = h.Value
		}
	}

	// Set fields from proto
	req.URL = parsedURL
	req.Host = hostHeader
	if req.Host == "" {
		req.Host = parsedURL.Host
	}
	req.RequestURI = protoReq.Url // Useful for client-side requests

	// Copy additional fields from sourceReq if provided
	if sourceReq != nil {
		req.Proto = sourceReq.Proto
		req.ProtoMajor = sourceReq.ProtoMajor
		req.ProtoMinor = sourceReq.ProtoMinor
		req.Close = sourceReq.Close
		req.Form = sourceReq.Form
		req.PostForm = sourceReq.PostForm
		req.MultipartForm = sourceReq.MultipartForm
		req.Trailer = sourceReq.Trailer
		req.TransferEncoding = sourceReq.TransferEncoding
		req.RemoteAddr = sourceReq.RemoteAddr
		req.TLS = sourceReq.TLS
	} else {
		// Fallback defaults if no sourceReq
		req.Proto = "HTTP/1.1"
		req.ProtoMajor = 1
		req.ProtoMinor = 1
		req.Close = false
	}

	// Set request ID
	req = ids.SetRequestID(req, protoReq.Id)

	return req, nil
}

// ToProtoResponse converts an http.Response to a proto HttpResponse.
func ToProtoResponse(resp *http.Response) *pb.HttpResponse {
	headers := []*pb.Header{}
	for k, vs := range resp.Header {
		for _, v := range vs {
			headers = append(headers, &pb.Header{Name: k, Value: v})
		}
	}
	var body []byte
	if resp.Body != nil {
		body, _ = io.ReadAll(resp.Body)
		resp.Body = io.NopCloser(bytes.NewBuffer(body))
	}
	return &pb.HttpResponse{
		Id:         ids.GetResponseID(resp),
		StatusCode: int32(resp.StatusCode),
		Headers:    headers,
		Body:       body,
	}
}

// FromProtoResponse converts a proto HttpResponse to an http.Response.
func FromProtoResponse(protoResp *pb.HttpResponse, req *http.Request) (*http.Response, error) {
	resp := &http.Response{
		StatusCode:    int(protoResp.StatusCode),
		Header:        make(http.Header),
		Body:          io.NopCloser(bytes.NewReader(protoResp.Body)),
		Request:       req,
		ContentLength: int64(len(protoResp.Body)),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Close:         false, // Default to keep-alive
	}

	// Set headers
	for _, h := range protoResp.Headers {
		resp.Header.Add(h.Name, h.Value)
	}

	// Set status text
	resp.Status = http.StatusText(int(protoResp.StatusCode))
	if resp.Status == "" {
		resp.Status = strconv.Itoa(int(protoResp.StatusCode)) + " Unknown"
	}

	return resp, nil
}
