package scope

import (
	"net/http"
	"path"
	"regexp"
	"strings"
)

type Scope struct {
	domainRe           *regexp.Regexp
	excludedExtensions map[string]bool
}

func New(domainRe *regexp.Regexp, excludedExtensions []string) *Scope {
	excluded := map[string]bool{}
	for _, ex := range excludedExtensions {
		ex = "." + strings.ToLower(ex)
		excluded[ex] = true
	}

	return &Scope{
		domainRe:           domainRe,
		excludedExtensions: excluded,
	}
}

func (s *Scope) IsInScope(r *http.Request) bool {
	return !s.isExcludedExtension(r) && s.isIncludedDomain(r)
}

func (s *Scope) isExcludedExtension(r *http.Request) bool {
	urlPath := r.URL.Path
	ext := strings.ToLower(path.Ext(urlPath))
	if ext == "" {
		return false
	}

	return s.excludedExtensions[ext]
}

func (s *Scope) isIncludedDomain(r *http.Request) bool {
	if s.domainRe == nil {
		return true
	}

	host := r.Host
	if host == "" {
		host = r.Header.Get("host")
	}

	if host == "" {
		return false
	}

	return s.domainRe.MatchString(host)
}
