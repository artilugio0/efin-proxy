package hooks

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/artilugio0/efin-proxy/internal/ids"
	_ "modernc.org/sqlite" // SQLite driver
)

func TestInitDatabase(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open in-memory database: %v", err)
	}
	defer db.Close()

	err = InitDatabase(db)
	if err != nil {
		t.Fatalf("InitDatabase failed: %v", err)
	}

	// Verify tables exist
	tables := []string{"requests", "responses", "headers", "cookies"}
	for _, table := range tables {
		var name string
		err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil || name != table {
			t.Errorf("Table %s not created", table)
		}
	}

	// Verify indexes exist
	indexes := []string{
		"idx_requests_request_id",
		"idx_responses_request_id",
		"idx_requests_url",
		"idx_responses_status_code",
		"idx_headers_name",
		"idx_headers_value",
		"idx_cookies_name",
		"idx_cookies_value",
	}
	for _, index := range indexes {
		var name string
		err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='index' AND name=?", index).Scan(&name)
		if err != nil || name != index {
			t.Errorf("Index %s not created", index)
		}
	}
}

func TestSaveRequestToDB(t *testing.T) {
	dbF, err := os.CreateTemp("", "tmpfile-")
	if err != nil {
		t.Fatalf("could not create db file: %v", err)
	}
	defer dbF.Close()
	defer os.Remove(dbF.Name())
	dbFile := dbF.Name()

	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		t.Fatalf("Failed to open tmp database: %v", err)
	}
	defer db.Close()

	err = InitDatabase(db)
	if err != nil {
		t.Fatalf("InitDatabase failed: %v", err)
	}

	// Create a sample request
	req := httptest.NewRequest("GET", "http://example.com/path", strings.NewReader("test body"))
	req.Header.Set("User-Agent", "test-agent")
	req.Header.Set("Cookie", "session=abc123")
	req.Host = "example.com" // Set Host field explicitly
	req = ids.SetRequestID(req, "test-request-id")

	// Save the request
	err = saveRequestToDB(dbFile, req)
	if err != nil {
		t.Fatalf("saveRequestToDB failed: %v", err)
	}

	// Verify request data
	var method, url, body string
	var timestamp time.Time
	err = db.QueryRow("SELECT method, url, body, timestamp FROM requests WHERE request_id = ?", "test-request-id").Scan(&method, &url, &body, &timestamp)
	if err != nil {
		t.Fatalf("Failed to query request: %v", err)
	}
	if method != "GET" || url != "http://example.com/path" || body != "test body" || timestamp.IsZero() {
		t.Errorf("Request data mismatch: got method=%s, url=%s, body=%s, timestamp=%v", method, url, body, timestamp)
	}

	// Verify headers (including Host)
	headers := map[string]string{}
	rows, err := db.Query("SELECT name, value FROM headers WHERE request_id = ?", "test-request-id")
	if err != nil {
		t.Fatalf("Failed to query headers: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			t.Fatalf("Failed to scan header: %v", err)
		}
		headers[name] = value
	}
	if headers["User-Agent"] != "test-agent" {
		t.Errorf("Expected User-Agent header 'test-agent', got %s", headers["User-Agent"])
	}
	if headers["Cookie"] != "session=abc123" {
		t.Errorf("Expected Cookie header 'session=abc123', got %s", headers["Cookie"])
	}
	if headers["Host"] != "example.com" {
		t.Errorf("Expected Host header 'example.com', got %s", headers["Host"])
	}

	// Verify cookies
	cookies := map[string]string{}
	rows, err = db.Query("SELECT name, value FROM cookies WHERE request_id = ?", "test-request-id")
	if err != nil {
		t.Fatalf("Failed to query cookies: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			t.Fatalf("Failed to scan cookie: %v", err)
		}
		cookies[name] = value
	}
	if cookies["session"] != "abc123" {
		t.Errorf("Expected cookie 'session=abc123', got %s", cookies["session"])
	}
}

func TestSaveResponseToDB(t *testing.T) {
	dbF, err := os.CreateTemp("", "tmpfile-")
	if err != nil {
		t.Fatalf("could not create db file: %v", err)
	}
	defer dbF.Close()
	defer os.Remove(dbF.Name())
	dbFile := dbF.Name()

	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		t.Fatalf("Failed to open tmp database: %v", err)
	}
	defer db.Close()

	err = InitDatabase(db)
	if err != nil {
		t.Fatalf("InitDatabase failed: %v", err)
	}

	// Create a sample response
	w := httptest.NewRecorder()
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Set-Cookie", "user=testuser; Path=/")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("response body"))
	resp := w.Result()
	resp.Request = httptest.NewRequest("GET", "http://example.com", nil)
	resp.Request = ids.SetRequestID(resp.Request, "test-response-id")

	// Save the response
	err = saveResponseToDB(dbFile, resp)
	if err != nil {
		t.Fatalf("saveResponseToDB failed: %v", err)
	}

	// Verify response data
	var statusCode int
	var body string
	var contentLength int64
	err = db.QueryRow("SELECT status_code, body, content_length FROM responses WHERE response_id = ?", "test-response-id").Scan(&statusCode, &body, &contentLength)
	if err != nil {
		t.Fatalf("Failed to query response: %v", err)
	}
	if statusCode != 200 || body != "response body" || contentLength != int64(len("response body")) {
		t.Errorf("Response data mismatch: got status=%d, body=%s, content_length=%d", statusCode, body, contentLength)
	}

	// Verify headers
	headers := map[string]string{}
	rows, err := db.Query("SELECT name, value FROM headers WHERE response_id = ?", "test-response-id")
	if err != nil {
		t.Fatalf("Failed to query headers: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			t.Fatalf("Failed to scan header: %v", err)
		}
		headers[name] = value
	}
	if headers["Content-Type"] != "text/plain" {
		t.Errorf("Expected Content-Type header 'text/plain', got %s", headers["Content-Type"])
	}
	if headers["Set-Cookie"] != "user=testuser; Path=/" {
		t.Errorf("Expected Set-Cookie header 'user=testuser; Path=/', got %s", headers["Set-Cookie"])
	}

	// Verify cookies
	cookies := map[string]string{}
	rows, err = db.Query("SELECT name, value FROM cookies WHERE response_id = ?", "test-response-id")
	if err != nil {
		t.Fatalf("Failed to query cookies: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			t.Fatalf("Failed to scan cookie: %v", err)
		}
		cookies[name] = value
	}
	if cookies["user"] != "testuser" {
		t.Errorf("Expected cookie 'user=testuser', got %s", cookies["user"])
	}
}
