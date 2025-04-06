package websockets

import (
	"net/http/httptest"
	"testing"
)

// TestIsWebSocketRequest tests the IsWebSocketRequest function with various header combinations
func TestIsWebSocketRequest(t *testing.T) {
	tests := []struct {
		name             string
		upgradeHeader    string
		connectionHeader string
		expectWebSocket  bool
	}{
		{
			name:             "Valid WebSocket request",
			upgradeHeader:    "websocket",
			connectionHeader: "Upgrade",
			expectWebSocket:  true,
		},
		{
			name:             "Valid WebSocket request with mixed case",
			upgradeHeader:    "WebSocket",
			connectionHeader: "upgrade",
			expectWebSocket:  true,
		},
		{
			name:             "Valid WebSocket request with multiple Connection values",
			upgradeHeader:    "websocket",
			connectionHeader: "keep-alive, Upgrade",
			expectWebSocket:  true,
		},
		{
			name:             "Missing Upgrade header",
			upgradeHeader:    "",
			connectionHeader: "Upgrade",
			expectWebSocket:  false,
		},
		{
			name:             "Missing Connection header",
			upgradeHeader:    "websocket",
			connectionHeader: "",
			expectWebSocket:  false,
		},
		{
			name:             "Wrong Upgrade value",
			upgradeHeader:    "http",
			connectionHeader: "Upgrade",
			expectWebSocket:  false,
		},
		{
			name:             "Wrong Connection value",
			upgradeHeader:    "websocket",
			connectionHeader: "keep-alive",
			expectWebSocket:  false,
		},
		{
			name:             "Empty headers",
			upgradeHeader:    "",
			connectionHeader: "",
			expectWebSocket:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://example.com", nil)
			if tt.upgradeHeader != "" {
				req.Header.Set("Upgrade", tt.upgradeHeader)
			}
			if tt.connectionHeader != "" {
				req.Header.Set("Connection", tt.connectionHeader)
			}

			result := IsWebSocketRequest(req)
			if result != tt.expectWebSocket {
				t.Errorf("IsWebSocketRequest() = %v, want %v for Upgrade: %q, Connection: %q",
					result, tt.expectWebSocket, tt.upgradeHeader, tt.connectionHeader)
			}
		})
	}
}

// TestIsWebSocketRequestCaseInsensitivity tests case insensitivity of header values
func TestIsWebSocketRequestCaseInsensitivity(t *testing.T) {
	variations := []struct {
		upgrade    string
		connection string
	}{
		{"websocket", "Upgrade"},
		{"WEBSOCKET", "UPGRADE"},
		{"WebSocket", "upgrade"},
		{"wEbSoCkEt", "UpGrAdE"},
	}

	for _, v := range variations {
		t.Run("Upgrade: "+v.upgrade+", Connection: "+v.connection, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://example.com", nil)
			req.Header.Set("Upgrade", v.upgrade)
			req.Header.Set("Connection", v.connection)

			if !IsWebSocketRequest(req) {
				t.Errorf("Expected WebSocket request with Upgrade: %q, Connection: %q, got false", v.upgrade, v.connection)
			}
		})
	}
}
