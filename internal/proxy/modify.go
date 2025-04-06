package proxy

import "net/http"

// ModifyRequest modifies the request to redirect to Google search
func ModifyRequest(req *http.Request) *http.Request {
	return req
}
