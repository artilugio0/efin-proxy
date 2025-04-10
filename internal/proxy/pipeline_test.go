package proxy

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/artilugio0/proxy-vibes/internal/certs"
)

// TestProcessRequestPipelines tests the pipeline processing with various configurations
func TestProcessRequestPipelines(t *testing.T) {
	// Generate a dummy Root CA for Proxy initialization
	rootCA, rootKey, _, _, err := certs.GenerateRootCA()
	if err != nil {
		t.Fatalf("Failed to generate Root CA: %v", err)
	}

	tests := []struct {
		name            string
		inPipeline      []ReadOnlyHook[*http.Request]
		modPipeline     []RequestModHook
		outPipeline     []ReadOnlyHook[*http.Request]
		expectError     bool
		expectModified  bool
		expectInFlag    bool
		expectModHeader string
		expectOutFlag   bool
	}{
		// Empty pipelines
		{
			name:            "All pipelines empty",
			inPipeline:      []ReadOnlyHook[*http.Request]{},
			modPipeline:     []RequestModHook{},
			outPipeline:     []ReadOnlyHook[*http.Request]{},
			expectError:     false,
			expectModified:  false,
			expectInFlag:    false,
			expectModHeader: "",
			expectOutFlag:   false,
		},
		// RequestInPipeline only
		{
			name:            "RequestInPipeline with 0 functions",
			inPipeline:      []ReadOnlyHook[*http.Request]{},
			modPipeline:     []RequestModHook{},
			outPipeline:     []ReadOnlyHook[*http.Request]{},
			expectError:     false,
			expectModified:  false,
			expectInFlag:    false,
			expectModHeader: "",
			expectOutFlag:   false,
		},
		{
			name: "RequestInPipeline with 1 function",
			inPipeline: []ReadOnlyHook[*http.Request]{
				func(req *http.Request) error {
					req.Method = "POST" // This should not persist due to cloning
					return nil
				},
			},
			modPipeline:     []RequestModHook{},
			outPipeline:     []ReadOnlyHook[*http.Request]{},
			expectError:     false,
			expectModified:  false,
			expectInFlag:    true,
			expectModHeader: "",
			expectOutFlag:   false,
		},
		{
			name: "RequestInPipeline with 2 functions",
			inPipeline: []ReadOnlyHook[*http.Request]{
				func(req *http.Request) error {
					req.Method = "POST" // Should not persist
					return nil
				},
				func(req *http.Request) error {
					req.Method = "PUT" // Should not persist
					return nil
				},
			},
			modPipeline:     []RequestModHook{},
			outPipeline:     []ReadOnlyHook[*http.Request]{},
			expectError:     false,
			expectModified:  false,
			expectInFlag:    true,
			expectModHeader: "",
			expectOutFlag:   false,
		},
		{
			name: "RequestInPipeline with error",
			inPipeline: []ReadOnlyHook[*http.Request]{
				func(req *http.Request) error {
					return errors.New("in error")
				},
			},
			modPipeline:     []RequestModHook{},
			outPipeline:     []ReadOnlyHook[*http.Request]{},
			expectError:     false, // Do not throw error if readonly fails, request can still be proccessed
			expectModified:  false,
			expectInFlag:    true,
			expectModHeader: "",
			expectOutFlag:   false,
		},
		// RequestModPipeline only
		{
			name:            "RequestModPipeline with 0 functions",
			inPipeline:      []ReadOnlyHook[*http.Request]{},
			modPipeline:     []RequestModHook{},
			outPipeline:     []ReadOnlyHook[*http.Request]{},
			expectError:     false,
			expectModified:  false,
			expectInFlag:    false,
			expectModHeader: "",
			expectOutFlag:   false,
		},
		{
			name:       "RequestModPipeline with 1 function",
			inPipeline: []ReadOnlyHook[*http.Request]{},
			modPipeline: []RequestModHook{
				func(req *http.Request) (*http.Request, error) {
					req.Header.Set("X-Mod", "mod1")
					return req, nil
				},
			},
			outPipeline:     []ReadOnlyHook[*http.Request]{},
			expectError:     false,
			expectModified:  true,
			expectInFlag:    false,
			expectModHeader: "mod1",
			expectOutFlag:   false,
		},
		{
			name:       "RequestModPipeline with 2 functions",
			inPipeline: []ReadOnlyHook[*http.Request]{},
			modPipeline: []RequestModHook{
				func(req *http.Request) (*http.Request, error) {
					req.Header.Set("X-Mod", "mod1")
					return req, nil
				},
				func(req *http.Request) (*http.Request, error) {
					req.Header.Set("X-Mod", "mod2")
					return req, nil
				},
			},
			outPipeline:     []ReadOnlyHook[*http.Request]{},
			expectError:     false,
			expectModified:  true,
			expectInFlag:    false,
			expectModHeader: "mod2",
			expectOutFlag:   false,
		},
		{
			name:       "RequestModPipeline with error",
			inPipeline: []ReadOnlyHook[*http.Request]{},
			modPipeline: []RequestModHook{
				func(req *http.Request) (*http.Request, error) {
					return nil, errors.New("mod error")
				},
			},
			outPipeline:     []ReadOnlyHook[*http.Request]{},
			expectError:     true,
			expectModified:  false,
			expectInFlag:    false,
			expectModHeader: "",
			expectOutFlag:   false,
		},
		// RequestOutPipeline only
		{
			name:            "RequestOutPipeline with 0 functions",
			inPipeline:      []ReadOnlyHook[*http.Request]{},
			modPipeline:     []RequestModHook{},
			outPipeline:     []ReadOnlyHook[*http.Request]{},
			expectError:     false,
			expectModified:  false,
			expectInFlag:    false,
			expectModHeader: "",
			expectOutFlag:   false,
		},
		{
			name:        "RequestOutPipeline with 1 function",
			inPipeline:  []ReadOnlyHook[*http.Request]{},
			modPipeline: []RequestModHook{},
			outPipeline: []ReadOnlyHook[*http.Request]{
				func(req *http.Request) error {
					req.Header.Set("X-Out", "out1") // Should not persist
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
			name:        "RequestOutPipeline with 2 functions",
			inPipeline:  []ReadOnlyHook[*http.Request]{},
			modPipeline: []RequestModHook{},
			outPipeline: []ReadOnlyHook[*http.Request]{
				func(req *http.Request) error {
					req.Header.Set("X-Out", "out1") // Should not persist
					return nil
				},
				func(req *http.Request) error {
					req.Header.Set("X-Out", "out2") // Should not persist
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
			name:        "RequestOutPipeline with error",
			inPipeline:  []ReadOnlyHook[*http.Request]{},
			modPipeline: []RequestModHook{},
			outPipeline: []ReadOnlyHook[*http.Request]{
				func(req *http.Request) error {
					return errors.New("out error")
				},
			},
			expectError:     false, // Do not throw error if readonly fails, request can still be proccessed
			expectModified:  false,
			expectInFlag:    false,
			expectModHeader: "",
			expectOutFlag:   true,
		},
		// All pipelines combined
		{
			name: "All pipelines with 1 function each",
			inPipeline: []ReadOnlyHook[*http.Request]{
				func(req *http.Request) error {
					req.Method = "POST" // Should not persist
					return nil
				},
			},
			modPipeline: []RequestModHook{
				func(req *http.Request) (*http.Request, error) {
					req.Header.Set("X-Mod", "mod")
					return req, nil
				},
			},
			outPipeline: []ReadOnlyHook[*http.Request]{
				func(req *http.Request) error {
					req.Method = "PUT" // Should not persist
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
			inPipeline: []ReadOnlyHook[*http.Request]{
				func(req *http.Request) error {
					req.Method = "POST" // Should not persist
					return nil
				},
				func(req *http.Request) error {
					req.Method = "PUT" // Should not persist
					return nil
				},
			},
			modPipeline: []RequestModHook{
				func(req *http.Request) (*http.Request, error) {
					req.Header.Set("X-Mod", "mod1")
					return req, nil
				},
				func(req *http.Request) (*http.Request, error) {
					req.Header.Set("X-Mod", "mod2")
					return req, nil
				},
			},
			outPipeline: []ReadOnlyHook[*http.Request]{
				func(req *http.Request) error {
					req.Method = "DELETE" // Should not persist
					return nil
				},
				func(req *http.Request) error {
					req.Method = "PATCH" // Should not persist
					return nil
				},
			},
			expectError:     false,
			expectModified:  true,
			expectInFlag:    true,
			expectModHeader: "mod2",
			expectOutFlag:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProxy(rootCA, rootKey)
			p.RequestModPipeline = tt.modPipeline

			// Flags to track pipeline execution
			inExecuted := false
			outExecuted := false

			// Wrap pipeline functions to set flags
			var wg sync.WaitGroup
			for i, fn := range tt.inPipeline {
				wg.Add(1)
				origFn := fn
				tt.inPipeline[i] = func(req *http.Request) error {
					defer wg.Done()
					inExecuted = true
					return origFn(req)
				}
			}
			for i, fn := range p.RequestModPipeline {
				wg.Add(1)
				origFn := fn
				p.RequestModPipeline[i] = func(req *http.Request) (*http.Request, error) {
					defer wg.Done()
					return origFn(req)
				}
			}
			for i, fn := range tt.outPipeline {
				wg.Add(1)
				origFn := fn
				tt.outPipeline[i] = func(req *http.Request) error {
					defer wg.Done()
					outExecuted = true
					return origFn(req)
				}
			}

			p.RequestInPipeline = newReadOnlyPipeline(tt.inPipeline)
			p.RequestOutPipeline = newReadOnlyPipeline(tt.outPipeline)

			req := httptest.NewRequest("GET", "http://example.com", nil)
			finalReq, err := p.processRequestPipelines(req)

			wg.Wait()

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

			// Check read-only pipelines didn’t modify the request (except via RequestModPipeline)
			if tt.expectModified {
				if finalReq == req {
					t.Errorf("Expected request to be modified, got same pointer: %p", req)
				}
				if finalReq.Header.Get("X-Mod") != tt.expectModHeader {
					t.Errorf("Expected X-Mod header to be %q, got %q", tt.expectModHeader, finalReq.Header.Get("X-Mod"))
				}
			} else {
				if finalReq != req && finalReq.Header.Get("X-Mod") != "" {
					t.Errorf("Expected request unchanged, got modified with X-Mod: %v", finalReq.Header.Get("X-Mod"))
				}
			}

			// Check pipeline execution flags
			if inExecuted != tt.expectInFlag {
				t.Errorf("Expected inExecuted to be %v, got %v", tt.expectInFlag, inExecuted)
			}
			if outExecuted != tt.expectOutFlag {
				t.Errorf("Expected outExecuted to be %v, got %v", tt.expectOutFlag, outExecuted)
			}

			// Verify read-only pipelines didn’t modify the request
			if finalReq.Method != req.Method && !tt.expectModified {
				t.Errorf("Read-only pipeline modified Method from %q to %q", req.Method, finalReq.Method)
			}
		})
	}
}
