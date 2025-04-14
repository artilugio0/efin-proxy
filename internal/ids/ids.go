package ids

import (
	"context"
	"net/http"
)

type requestIDKeyType struct{}

var requestIDKey = requestIDKeyType{}

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
