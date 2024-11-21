package efinproxy

import (
	"net/http"

	"github.com/artilugio0/efincore"
	"github.com/google/uuid"
)

func AddHeaderRequest(headerName, headerValue string) efincore.HookRequestMod {
	return efincore.HookRequestModFunc(func(r *http.Request, _ uuid.UUID) error {
		r.Header.Add(headerName, headerValue)
		return nil
	})
}

func SetHeaderRequest(headerName, headerValue string) efincore.HookRequestMod {
	return efincore.HookRequestModFunc(func(r *http.Request, _ uuid.UUID) error {
		r.Header.Set(headerName, headerValue)
		return nil
	})
}

func RemoveHeaderRequest(headerName string) efincore.HookRequestMod {
	return efincore.HookRequestModFunc(func(r *http.Request, _ uuid.UUID) error {
		r.Header.Del(headerName)
		return nil
	})
}

func RemoveHeaderResponse(headerName string) efincore.HookResponseMod {
	return efincore.HookResponseModFunc(func(r *http.Response, _ uuid.UUID) error {
		r.Header.Del(headerName)
		return nil
	})
}

func SetHeaderResponse(headerName, headerValue string) efincore.HookResponseMod {
	return efincore.HookResponseModFunc(func(r *http.Response, _ uuid.UUID) error {
		r.Header.Set(headerName, headerValue)
		return nil
	})
}

func AddHeaderResponse(headerName, headerValue string) efincore.HookResponseMod {
	return efincore.HookResponseModFunc(func(r *http.Response, _ uuid.UUID) error {
		r.Header.Add(headerName, headerValue)
		return nil
	})
}
