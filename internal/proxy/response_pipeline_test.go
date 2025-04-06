package proxy

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/artilugio0/proxy-vibes/internal/certs"
)

// TestProcessResponsePipelines tests the response pipeline processing with various configurations
func TestProcessResponsePipelines(t *testing.T) {
	// Generate a dummy Root CA for Proxy initialization
	rootCA, rootKey, _, _, err := certs.GenerateRootCA()
	if err != nil {
		t.Fatalf("Failed to generate Root CA: %v", err)
	}

	tests := []struct {
		name            string
		inPipeline      []ResponseInOutFunc
		modPipeline     []ResponseModFunc
		outPipeline     []ResponseInOutFunc
		expectError     bool
		expectModified  bool
		expectInFlag    bool
		expectModHeader string
		expectOutFlag   bool
	}{
		// Empty pipelines
		{
			name:            "All pipelines empty",
			inPipeline:      []ResponseInOutFunc{},
			modPipeline:     []ResponseModFunc{},
			outPipeline:     []ResponseInOutFunc{},
			expectError:     false,
			expectModified:  false,
			expectInFlag:    false,
			expectModHeader: "",
			expectOutFlag:   false,
		},
		// ResponseInPipeline only
		{
			name:            "ResponseInPipeline with 0 functions",
			inPipeline:      []ResponseInOutFunc{},
			modPipeline:     []ResponseModFunc{},
			outPipeline:     []ResponseInOutFunc{},
			expectError:     false,
			expectModified:  false,
			expectInFlag:    false,
			expectModHeader: "",
			expectOutFlag:   false,
		},
		{
			name: "ResponseInPipeline with 1 function",
			inPipeline: []ResponseInOutFunc{
				func(resp *http.Response) error {
					resp.StatusCode = 201 // Should not persist
					return nil
				},
			},
			modPipeline:     []ResponseModFunc{},
			outPipeline:     []ResponseInOutFunc{},
			expectError:     false,
			expectModified:  false,
			expectInFlag:    true,
			expectModHeader: "",
			expectOutFlag:   false,
		},
		{
			name: "ResponseInPipeline with 2 functions",
			inPipeline: []ResponseInOutFunc{
				func(resp *http.Response) error {
					resp.StatusCode = 201 // Should not persist
					return nil
				},
				func(resp *http.Response) error {
					resp.StatusCode = 202 // Should not persist
					return nil
				},
			},
			modPipeline:     []ResponseModFunc{},
			outPipeline:     []ResponseInOutFunc{},
			expectError:     false,
			expectModified:  false,
			expectInFlag:    true,
			expectModHeader: "",
			expectOutFlag:   false,
		},
		{
			name: "ResponseInPipeline with error",
			inPipeline: []ResponseInOutFunc{
				func(resp *http.Response) error {
					return errors.New("in error")
				},
			},
			modPipeline:     []ResponseModFunc{},
			outPipeline:     []ResponseInOutFunc{},
			expectError:     true,
			expectModified:  false,
			expectInFlag:    false,
			expectModHeader: "",
			expectOutFlag:   false,
		},
		// ResponseModPipeline only
		{
			name:            "ResponseModPipeline with 0 functions",
			inPipeline:      []ResponseInOutFunc{},
			modPipeline:     []ResponseModFunc{},
			outPipeline:     []ResponseInOutFunc{},
			expectError:     false,
			expectModified:  false,
			expectInFlag:    false,
			expectModHeader: "",
			expectOutFlag:   false,
		},
		{
			name:       "ResponseModPipeline with 1 function",
			inPipeline: []ResponseInOutFunc{},
			modPipeline: []ResponseModFunc{
				func(resp *http.Response) (*http.Response, error) {
					resp.Header.Set("X-Mod", "mod1")
					return resp, nil
				},
			},
			outPipeline:     []ResponseInOutFunc{},
			expectError:     false,
			expectModified:  true,
			expectInFlag:    false,
			expectModHeader: "mod1",
			expectOutFlag:   false,
		},
		{
			name:       "ResponseModPipeline with 2 functions",
			inPipeline: []ResponseInOutFunc{},
			modPipeline: []ResponseModFunc{
				func(resp *http.Response) (*http.Response, error) {
					resp.Header.Set("X-Mod", "mod1")
					return resp, nil
				},
				func(resp *http.Response) (*http.Response, error) {
					resp.Header.Set("X-Mod", "mod2")
					return resp, nil
				},
			},
			outPipeline:     []ResponseInOutFunc{},
			expectError:     false,
			expectModified:  true,
			expectInFlag:    false,
			expectModHeader: "mod2",
			expectOutFlag:   false,
		},
		{
			name:       "ResponseModPipeline with error",
			inPipeline: []ResponseInOutFunc{},
			modPipeline: []ResponseModFunc{
				func(resp *http.Response) (*http.Response, error) {
					return nil, errors.New("mod error")
				},
			},
			outPipeline:     []ResponseInOutFunc{},
			expectError:     true,
			expectModified:  false,
			expectInFlag:    false,
			expectModHeader: "",
			expectOutFlag:   false,
		},
		// ResponseOutPipeline only
		{
			name:            "ResponseOutPipeline with 0 functions",
			inPipeline:      []ResponseInOutFunc{},
			modPipeline:     []ResponseModFunc{},
			outPipeline:     []ResponseInOutFunc{},
			expectError:     false,
			expectModified:  false,
			expectInFlag:    false,
			expectModHeader: "",
			expectOutFlag:   false,
		},
		{
			name:        "ResponseOutPipeline with 1 function",
			inPipeline:  []ResponseInOutFunc{},
			modPipeline: []ResponseModFunc{},
			outPipeline: []ResponseInOutFunc{
				func(resp *http.Response) error {
					resp.StatusCode = 201 // Should not persist
					return nil
				},
			},
			expectError:     false,
			expectModified:  false,
			expectInFlag:    false,
			expectModHeader: "",
			expectOutFlag:   true,
		},
		{
			name:        "ResponseOutPipeline with 2 functions",
			inPipeline:  []ResponseInOutFunc{},
			modPipeline: []ResponseModFunc{},
			outPipeline: []ResponseInOutFunc{
				func(resp *http.Response) error {
					resp.StatusCode = 201 // Should not persist
					return nil
				},
				func(resp *http.Response) error {
					resp.StatusCode = 202 // Should not persist
					return nil
				},
			},
			expectError:     false,
			expectModified:  false,
			expectInFlag:    false,
			expectModHeader: "",
			expectOutFlag:   true,
		},
		{
			name:        "ResponseOutPipeline with error",
			inPipeline:  []ResponseInOutFunc{},
			modPipeline: []ResponseModFunc{},
			outPipeline: []ResponseInOutFunc{
				func(resp *http.Response) error {
					return errors.New("out error")
				},
			},
			expectError:     true,
			expectModified:  false,
			expectInFlag:    false,
			expectModHeader: "",
			expectOutFlag:   false,
		},
		// All pipelines combined
		{
			name: "All pipelines with 1 function each",
			inPipeline: []ResponseInOutFunc{
				func(resp *http.Response) error {
					resp.StatusCode = 201 // Should not persist
					return nil
				},
			},
			modPipeline: []ResponseModFunc{
				func(resp *http.Response) (*http.Response, error) {
					resp.Header.Set("X-Mod", "mod")
					return resp, nil
				},
			},
			outPipeline: []ResponseInOutFunc{
				func(resp *http.Response) error {
					resp.StatusCode = 202 // Should not persist
					return nil
				},
			},
			expectError:     false,
			expectModified:  true,
			expectInFlag:    true,
			expectModHeader: "mod",
			expectOutFlag:   true,
		},
		{
			name: "All pipelines with multiple functions",
			inPipeline: []ResponseInOutFunc{
				func(resp *http.Response) error {
					resp.StatusCode = 201 // Should not persist
					return nil
				},
				func(resp *http.Response) error {
					resp.StatusCode = 202 // Should not persist
					return nil
				},
			},
			modPipeline: []ResponseModFunc{
				func(resp *http.Response) (*http.Response, error) {
					resp.Header.Set("X-Mod", "mod1")
					return resp, nil
				},
				func(resp *http.Response) (*http.Response, error) {
					resp.Header.Set("X-Mod", "mod2")
					return resp, nil
				},
			},
			outPipeline: []ResponseInOutFunc{
				func(resp *http.Response) error {
					resp.StatusCode = 203 // Should not persist
					return nil
				},
				func(resp *http.Response) error {
					resp.StatusCode = 204 // Should not persist
					return nil
				},
			},
			expectError:     false,
			expectModified:  true,
			expectInFlag:    true,
			expectModHeader: "mod2",
			expectOutFlag:   true,
		},
		{
			name: "All pipelines with error in middle",
			inPipeline: []ResponseInOutFunc{
				func(resp *http.Response) error {
					resp.StatusCode = 201 // Should not persist
					return nil
				},
			},
			modPipeline: []ResponseModFunc{
				func(resp *http.Response) (*http.Response, error) {
					return nil, errors.New("mod error")
				},
			},
			outPipeline: []ResponseInOutFunc{
				func(resp *http.Response) error {
					resp.StatusCode = 202 // Should not persist
					return nil
				},
			},
			expectError:     true,
			expectModified:  false,
			expectInFlag:    true,
			expectModHeader: "",
			expectOutFlag:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProxy(rootCA, rootKey)
			p.ResponseInPipeline = tt.inPipeline
			p.ResponseModPipeline = tt.modPipeline
			p.ResponseOutPipeline = tt.outPipeline

			// Flags to track pipeline execution
			inExecuted := false
			outExecuted := false

			// Wrap pipeline functions to set flags
			for i, fn := range p.ResponseInPipeline {
				origFn := fn
				p.ResponseInPipeline[i] = func(resp *http.Response) error {
					inExecuted = true
					return origFn(resp)
				}
			}
			for i, fn := range p.ResponseOutPipeline {
				origFn := fn
				p.ResponseOutPipeline[i] = func(resp *http.Response) error {
					outExecuted = true
					return origFn(resp)
				}
			}

			// Create a sample response
			resp := &http.Response{
				StatusCode:    200,
				Header:        make(http.Header),
				Body:          io.NopCloser(bytes.NewBufferString("test")),
				ContentLength: 4,
			}

			finalResp, err := p.processResponsePipelines(resp)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected an error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Check read-only pipelines didn’t modify the response (except via ResponseModPipeline)
			if tt.expectModified {
				if finalResp == resp {
					t.Errorf("Expected response to be modified, got same pointer: %p", resp)
				}
				if finalResp.Header.Get("X-Mod") != tt.expectModHeader {
					t.Errorf("Expected X-Mod header to be %q, got %q", tt.expectModHeader, finalResp.Header.Get("X-Mod"))
				}
			} else {
				if finalResp != resp && finalResp.Header.Get("X-Mod") != "" {
					t.Errorf("Expected response unchanged, got modified with X-Mod: %v", finalResp.Header.Get("X-Mod"))
				}
			}

			// Check pipeline execution flags
			if inExecuted != tt.expectInFlag {
				t.Errorf("Expected inExecuted to be %v, got %v", tt.expectInFlag, inExecuted)
			}
			if outExecuted != tt.expectOutFlag {
				t.Errorf("Expected outExecuted to be %v, got %v", tt.expectOutFlag, outExecuted)
			}

			// Verify read-only pipelines didn’t modify the response
			if finalResp.StatusCode != resp.StatusCode && !tt.expectModified {
				t.Errorf("Read-only pipeline modified StatusCode from %d to %d", resp.StatusCode, finalResp.StatusCode)
			}
		})
	}
}
