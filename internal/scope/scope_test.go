package scope

import (
	"net/http"
	"regexp"
	"testing"
)

func TestIsInScope(t *testing.T) {
	tt := []struct {
		desc               string
		extension          string
		domain             string
		domainRe           *regexp.Regexp
		excludedExtensions []string
		isInScope          bool
	}{
		{
			desc:               "no restrictions",
			extension:          "png",
			domain:             "www.example.com",
			domainRe:           nil,
			excludedExtensions: []string{},
			isInScope:          true,
		},
		{
			desc:               "excluded extension",
			extension:          "png",
			domain:             "www.example.com",
			domainRe:           nil,
			excludedExtensions: []string{"png"},
			isInScope:          false,
		},
		{
			desc:               "non excluded extension",
			extension:          "txt",
			domain:             "www.example.com",
			domainRe:           nil,
			excludedExtensions: []string{"png"},
			isInScope:          true,
		},
		{
			desc:               "empty excluded extension",
			extension:          "txt",
			domain:             "www.example.com",
			domainRe:           nil,
			excludedExtensions: []string{},
			isInScope:          true,
		},
		{
			desc:               "in scope domain",
			extension:          "",
			domain:             "www.example.com",
			domainRe:           regexp.MustCompile(`.*\.example\.com$`),
			excludedExtensions: []string{},
			isInScope:          true,
		},
		{
			desc:               "out of scope domain",
			extension:          "",
			domain:             "www.outofscope.com",
			domainRe:           regexp.MustCompile(`.*\.example\.com$`),
			excludedExtensions: []string{},
			isInScope:          false,
		},
		{
			desc:               "ok ext and in scope domain",
			extension:          "png",
			domain:             "www.example.com",
			domainRe:           regexp.MustCompile(`^www\.example\.com$`),
			excludedExtensions: []string{},
			isInScope:          true,
		},
		{
			desc:               "excluded ext and in scope domain",
			extension:          "png",
			domain:             "www.example.com",
			domainRe:           regexp.MustCompile(`^www\.example\.com$`),
			excludedExtensions: []string{"png"},
			isInScope:          false,
		},
		{
			desc:               "ok ext and not in scope domain",
			extension:          "png",
			domain:             "www.outofscope.com",
			domainRe:           regexp.MustCompile(`^www\.example\.com$`),
			excludedExtensions: []string{""},
			isInScope:          false,
		},
		{
			desc:               "excluded ext and not in scope domain",
			extension:          "png",
			domain:             "www.outofscope.com",
			domainRe:           regexp.MustCompile(`^www\.example\.com$`),
			excludedExtensions: []string{"png"},
			isInScope:          false,
		},
	}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			scope := New(tc.domainRe, tc.excludedExtensions)
			path := "/test"
			if tc.extension != "" {
				path += "." + tc.extension
			}

			req, err := http.NewRequest("GET", "https://"+tc.domain+path, nil)
			if err != nil {
				t.Fatalf("could not create request: %v", err)
			}

			got := scope.IsInScope(req)

			if got != tc.isInScope {
				t.Errorf("got '%t', isInScope '%t'", got, tc.isInScope)
			}
		})
	}
}
