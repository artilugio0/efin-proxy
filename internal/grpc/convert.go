package grpc

import (
	"bytes"
	"io"
	"net/http"

	pb "github.com/artilugio0/proxy-vibes/internal/grpc/proto"
	"github.com/artilugio0/proxy-vibes/internal/ids"
)

// ToProtoRequest converts an http.Request to a proto HttpRequest.
func ToProtoRequest(req *http.Request) *pb.HttpRequest {
	headers := []*pb.Header{}
	for k, vs := range req.Header {
		for _, v := range vs {
			headers = append(headers, &pb.Header{Name: k, Value: v})
		}
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

// FromProtoRequest converts a proto HttpRequest to an http.Request.
func FromProtoRequest(protoReq *pb.HttpRequest) (*http.Request, error) {
	req, err := http.NewRequest(protoReq.Method, protoReq.Url, io.NopCloser(bytes.NewReader(protoReq.Body)))
	if err != nil {
		return nil, err
	}
	for _, h := range protoReq.Headers {
		req.Header.Add(h.Name, h.Value)
	}
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
		StatusCode: int(protoResp.StatusCode),
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(protoResp.Body)),
	}
	for _, h := range protoResp.Headers {
		resp.Header.Add(h.Name, h.Value)
	}
	resp.Request = req
	return resp, nil
}
