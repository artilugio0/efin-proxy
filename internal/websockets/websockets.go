package websockets

import (
	"net/http"
	"strings"
)

// IsWebSocketRequest checks if the request is a WebSocket upgrade request
func IsWebSocketRequest(req *http.Request) bool {
	upgrade := strings.ToLower(req.Header.Get("Upgrade"))
	connection := strings.ToLower(req.Header.Get("Connection"))
	return upgrade == "websocket" && strings.Contains(connection, "upgrade")
}
