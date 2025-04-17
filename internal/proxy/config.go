package proxy

import (
	"log"
	"net/http"
	"regexp"

	"github.com/artilugio0/proxy-vibes/internal/hooks"
	"github.com/artilugio0/proxy-vibes/internal/pipeline"
	"github.com/artilugio0/proxy-vibes/internal/scope"
)

type Config struct {
	DBFile    string
	PrintLogs bool
	SaveDir   string

	DomainRe           string
	ExcludedExtensions []string

	RequestInHooks  []pipeline.ReadOnlyHook[*http.Request]
	RequestModHooks []pipeline.ModHook[*http.Request]
	RequestOutHooks []pipeline.ReadOnlyHook[*http.Request]

	ResponseInHooks  []pipeline.ReadOnlyHook[*http.Response]
	ResponseModHooks []pipeline.ModHook[*http.Response]
	ResponseOutHooks []pipeline.ReadOnlyHook[*http.Response]
}

func (c *Config) Apply(p *Proxy) error {
	requestInHooks := append([]pipeline.ReadOnlyHook[*http.Request]{}, c.RequestInHooks...)
	requestModHooks := append([]pipeline.ModHook[*http.Request]{}, c.RequestModHooks...)
	requestOutHooks := append([]pipeline.ReadOnlyHook[*http.Request]{}, c.RequestOutHooks...)
	responseInHooks := append([]pipeline.ReadOnlyHook[*http.Response]{}, c.ResponseInHooks...)
	responseModHooks := append([]pipeline.ModHook[*http.Response]{}, c.ResponseModHooks...)
	responseOutHooks := append([]pipeline.ReadOnlyHook[*http.Response]{}, c.ResponseOutHooks...)

	// Add logging hooks if -p is set
	if c.PrintLogs {
		requestOutHooks = append(requestOutHooks, hooks.LogRawRequest)
		responseInHooks = append(responseInHooks, hooks.LogRawResponse)
		log.Printf("Enabled raw request/response logging to stdout")
	}

	// Add Accept-Encoding removal hook
	requestModHooks = append(requestModHooks, func(r *http.Request) (*http.Request, error) {
		r.Header.Del("Accept-Encoding")
		return r, nil
	})

	// Add database save hooks if database is initialized
	if c.DBFile != "" {
		saveRequest, saveResponse := hooks.NewDBSaveHooks(c.DBFile)
		requestOutHooks = append(requestOutHooks, saveRequest)
		responseInHooks = append(responseInHooks, saveResponse)
		log.Printf("Saving requests and responses to database at %s", c.DBFile)
	}

	// Add file save hooks if directory is specified
	if c.SaveDir != "" {
		saveRequest, saveResponse := hooks.NewFileSaveHooks(c.SaveDir)
		requestOutHooks = append(requestOutHooks, saveRequest)
		responseInHooks = append(responseInHooks, saveResponse)
		log.Printf("Saving requests and responses to directory: %s", c.SaveDir)
	}

	var domainRe *regexp.Regexp
	if c.DomainRe != "" {
		var err error
		domainRe, err = regexp.Compile(c.DomainRe)
		if err != nil {
			return err
		}
	}
	scope := scope.New(domainRe, c.ExcludedExtensions)
	p.SetScope(scope.IsInScope)

	p.SetRequestInHooks(requestInHooks)
	p.SetRequestModHooks(requestModHooks)
	p.SetRequestOutHooks(requestOutHooks)
	p.SetResponseInHooks(responseInHooks)
	p.SetResponseModHooks(responseModHooks)
	p.SetResponseOutHooks(responseOutHooks)

	return nil
}
