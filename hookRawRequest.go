package efinproxy

import (
	"net/http"
	"os"
	"path"

	"github.com/google/uuid"
)

type RawRequestSaver struct {
	directory string
}

func NewRawRequestSaver(directory string) *RawRequestSaver {
	return &RawRequestSaver{
		directory: directory,
	}
}

func (rrs *RawRequestSaver) SaveRequest(r *http.Request, id uuid.UUID) error {
	bytes, err := FormatRequest(r)
	if err != nil {
		return err
	}

	name := id.String() + "-request"
	path := path.Join(rrs.directory, name)

	return os.WriteFile(path, bytes, 0644)
}

func (rrs *RawRequestSaver) SaveResponse(r *http.Response, id uuid.UUID) error {
	bytes, err := FormatResponse(r)
	if err != nil {
		return err
	}

	name := id.String() + "-response"
	path := path.Join(rrs.directory, name)

	return os.WriteFile(path, bytes, 0644)
}
