package efinproxy

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"sync"
	"text/template"

	"github.com/google/uuid"
)

type Printer struct {
	template *template.Template
	mutex    *sync.Mutex
}

func NewPrinter(tpl string) (*Printer, error) {
	t, err := template.New("").Parse(tpl + "\n")
	if err != nil {
		return nil, err
	}

	printer := &Printer{
		template: t,
		mutex:    &sync.Mutex{},
	}

	return printer, nil
}

func (p *Printer) PrintRequest(r *http.Request, id uuid.UUID) error {
	m := map[string]string{
		"type": "REQUEST",
		"end":  "START",
		"id":   id.String(),
	}

	if err := p.template.Execute(os.Stdout, m); err != nil {
		return err
	}

	formatted, err := FormatRequest(r)
	if err != nil {
		return err
	}

	buf := bytes.NewBuffer(formatted)
	if _, err := buf.WriteTo(os.Stdout); err != nil {
		return err
	}

	fmt.Println("")
	m["end"] = "END"
	if err := p.template.Execute(os.Stdout, m); err != nil {
		return err
	}

	return nil
}

func (p *Printer) PrintResponse(r *http.Response, id uuid.UUID) error {
	m := map[string]string{
		"type": "RESPONSE",
		"end":  "START",
		"id":   id.String(),
	}

	if err := p.template.Execute(os.Stdout, m); err != nil {
		return err
	}

	formatted, err := FormatResponse(r)
	if err != nil {
		return err
	}

	buf := bytes.NewBuffer(formatted)
	if _, err := buf.WriteTo(os.Stdout); err != nil {
		return err
	}

	fmt.Println("")
	m["end"] = "END"
	if err := p.template.Execute(os.Stdout, m); err != nil {
		return err
	}

	return nil
}
