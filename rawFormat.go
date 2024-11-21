package efinproxy

import (
	"bytes"
	"net/http"
	"strconv"

	"github.com/artilugio0/efincore"
)

func FormatRequest(r *http.Request) ([]byte, error) {
	u := *r.URL
	u.Host = ""
	u.Scheme = ""
	headers := r.Method + " " + u.String() + " " + r.Proto + "\r\n"
	for h, vs := range r.Header {
		for _, v := range vs {
			headers += h + ": " + v + "\r\n"
		}
	}
	headers += "\r\n"

	buf := bytes.NewBufferString(headers)

	// according to documentation this function never returns an error
	body, err := r.Body.(*efincore.RBody).GetBytes()
	if err != nil {
		return nil, err
	}
	buf.Write(body)

	return buf.Bytes(), nil
}

func FormatResponse(r *http.Response) ([]byte, error) {
	headers := r.Proto + " " + strconv.Itoa(r.StatusCode) + " " + http.StatusText(r.StatusCode) + "\r\n"
	for h, vs := range r.Header {
		for _, v := range vs {
			headers += h + ": " + v + "\r\n"
		}
	}
	headers += "\r\n"

	buf := bytes.NewBufferString(headers)

	// according to documentation this function never returns an error
	body, err := r.Body.(*efincore.RBody).GetBytes()
	if err != nil {
		return nil, err
	}
	buf.Write(body)

	return buf.Bytes(), nil
}
