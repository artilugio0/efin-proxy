package proxy

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/artilugio0/efin-proxy/internal/certs"
	"github.com/artilugio0/efin-proxy/internal/pipeline"
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
		inPipeline      []pipeline.ReadOnlyHook[*http.Request]
		modPipeline     []pipeline.ModHook[*http.Request]
		outPipeline     []pipeline.ReadOnlyHook[*http.Request]
		expectError     bool
		expectModified  bool
		expectInFlag    bool
		expectModHeader string
		expectOutFlag   bool
	}{
		// Empty pipelines
		{
			name:            "All pipelines empty",
			inPipeline:      []pipeline.ReadOnlyHook[*http.Request]{},
			modPipeline:     []pipeline.ModHook[*http.Request]{},
			outPipeline:     []pipeline.ReadOnlyHook[*http.Request]{},
			expectError:     false,
			expectModified:  false,
			expectInFlag:    false,
			expectModHeader: "",
			expectOutFlag:   false,
		},
		// requestInPipeline only
		{
			name:            "requestInPipeline with 0 functions",
			inPipeline:      []pipeline.ReadOnlyHook[*http.Request]{},
			modPipeline:     []pipeline.ModHook[*http.Request]{},
			outPipeline:     []pipeline.ReadOnlyHook[*http.Request]{},
			expectError:     false,
			expectModified:  false,
			expectInFlag:    false,
			expectModHeader: "",
			expectOutFlag:   false,
		},
		{
			name: "requestInPipeline with 1 function",
			inPipeline: []pipeline.ReadOnlyHook[*http.Request]{
				func(req *http.Request) error {
					req.Method = "POST" // This should not persist due to cloning
					return nil
				},
			},
			modPipeline:     []pipeline.ModHook[*http.Request]{},
			outPipeline:     []pipeline.ReadOnlyHook[*http.Request]{},
			expectError:     false,
			expectModified:  false,
			expectInFlag:    true,
			expectModHeader: "",
			expectOutFlag:   false,
		},
		{
			name: "requestInPipeline with 2 functions",
			inPipeline: []pipeline.ReadOnlyHook[*http.Request]{
				func(req *http.Request) error {
					req.Method = "POST" // Should not persist
					return nil
				},
				func(req *http.Request) error {
					req.Method = "PUT" // Should not persist
					return nil
				},
			},
			modPipeline:     []pipeline.ModHook[*http.Request]{},
			outPipeline:     []pipeline.ReadOnlyHook[*http.Request]{},
			expectError:     false,
			expectModified:  false,
			expectInFlag:    true,
			expectModHeader: "",
			expectOutFlag:   false,
		},
		{
			name: "requestInPipeline with error",
			inPipeline: []pipeline.ReadOnlyHook[*http.Request]{
				func(req *http.Request) error {
					return errors.New("in error")
				},
			},
			modPipeline:     []pipeline.ModHook[*http.Request]{},
			outPipeline:     []pipeline.ReadOnlyHook[*http.Request]{},
			expectError:     false, // Do not throw error if readonly fails, request can still be proccessed
			expectModified:  false,
			expectInFlag:    true,
			expectModHeader: "",
			expectOutFlag:   false,
		},
		// requestModPipeline only
		{
			name:            "requestModPipeline with 0 functions",
			inPipeline:      []pipeline.ReadOnlyHook[*http.Request]{},
			modPipeline:     []pipeline.ModHook[*http.Request]{},
			outPipeline:     []pipeline.ReadOnlyHook[*http.Request]{},
			expectError:     false,
			expectModified:  false,
			expectInFlag:    false,
			expectModHeader: "",
			expectOutFlag:   false,
		},
		{
			name:       "requestModPipeline with 1 function",
			inPipeline: []pipeline.ReadOnlyHook[*http.Request]{},
			modPipeline: []pipeline.ModHook[*http.Request]{
				func(req *http.Request) (*http.Request, error) {
					req.Header.Set("X-Mod", "mod1")
					return req, nil
				},
			},
			outPipeline:     []pipeline.ReadOnlyHook[*http.Request]{},
			expectError:     false,
			expectModified:  true,
			expectInFlag:    false,
			expectModHeader: "mod1",
			expectOutFlag:   false,
		},
		{
			name:       "requestModPipeline with 2 functions",
			inPipeline: []pipeline.ReadOnlyHook[*http.Request]{},
			modPipeline: []pipeline.ModHook[*http.Request]{
				func(req *http.Request) (*http.Request, error) {
					req.Header.Set("X-Mod", "mod1")
					return req, nil
				},
				func(req *http.Request) (*http.Request, error) {
					req.Header.Set("X-Mod", "mod2")
					return req, nil
				},
			},
			outPipeline:     []pipeline.ReadOnlyHook[*http.Request]{},
			expectError:     false,
			expectModified:  true,
			expectInFlag:    false,
			expectModHeader: "mod2",
			expectOutFlag:   false,
		},
		{
			name:       "requestModPipeline with error",
			inPipeline: []pipeline.ReadOnlyHook[*http.Request]{},
			modPipeline: []pipeline.ModHook[*http.Request]{
				func(req *http.Request) (*http.Request, error) {
					return nil, errors.New("mod error")
				},
			},
			outPipeline:     []pipeline.ReadOnlyHook[*http.Request]{},
			expectError:     true,
			expectModified:  false,
			expectInFlag:    false,
			expectModHeader: "",
			expectOutFlag:   false,
		},
		// requestOutPipeline only
		{
			name:            "requestOutPipeline with 0 functions",
			inPipeline:      []pipeline.ReadOnlyHook[*http.Request]{},
			modPipeline:     []pipeline.ModHook[*http.Request]{},
			outPipeline:     []pipeline.ReadOnlyHook[*http.Request]{},
			expectError:     false,
			expectModified:  false,
			expectInFlag:    false,
			expectModHeader: "",
			expectOutFlag:   false,
		},
		{
			name:        "requestOutPipeline with 1 function",
			inPipeline:  []pipeline.ReadOnlyHook[*http.Request]{},
			modPipeline: []pipeline.ModHook[*http.Request]{},
			outPipeline: []pipeline.ReadOnlyHook[*http.Request]{
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
			name:        "requestOutPipeline with 2 functions",
			inPipeline:  []pipeline.ReadOnlyHook[*http.Request]{},
			modPipeline: []pipeline.ModHook[*http.Request]{},
			outPipeline: []pipeline.ReadOnlyHook[*http.Request]{
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
			name:        "requestOutPipeline with error",
			inPipeline:  []pipeline.ReadOnlyHook[*http.Request]{},
			modPipeline: []pipeline.ModHook[*http.Request]{},
			outPipeline: []pipeline.ReadOnlyHook[*http.Request]{
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
			inPipeline: []pipeline.ReadOnlyHook[*http.Request]{
				func(req *http.Request) error {
					req.Method = "POST" // Should not persist
					return nil
				},
			},
			modPipeline: []pipeline.ModHook[*http.Request]{
				func(req *http.Request) (*http.Request, error) {
					req.Header.Set("X-Mod", "mod")
					return req, nil
				},
			},
			outPipeline: []pipeline.ReadOnlyHook[*http.Request]{
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
			inPipeline: []pipeline.ReadOnlyHook[*http.Request]{
				func(req *http.Request) error {
					req.Method = "POST" // Should not persist
					return nil
				},
				func(req *http.Request) error {
					req.Method = "PUT" // Should not persist
					return nil
				},
			},
			modPipeline: []pipeline.ModHook[*http.Request]{
				func(req *http.Request) (*http.Request, error) {
					req.Header.Set("X-Mod", "mod1")
					return req, nil
				},
				func(req *http.Request) (*http.Request, error) {
					req.Header.Set("X-Mod", "mod2")
					return req, nil
				},
			},
			outPipeline: []pipeline.ReadOnlyHook[*http.Request]{
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
			for i, fn := range tt.modPipeline {
				wg.Add(1)
				origFn := fn
				tt.modPipeline[i] = func(req *http.Request) (*http.Request, error) {
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

			p.requestInPipeline = pipeline.NewReadOnlyPipeline(tt.inPipeline)
			p.requestModPipeline = pipeline.NewModPipeline(tt.modPipeline)
			p.requestOutPipeline = pipeline.NewReadOnlyPipeline(tt.outPipeline)

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

			// Check read-only pipelines didn’t modify the request (except via requestModPipeline)
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
