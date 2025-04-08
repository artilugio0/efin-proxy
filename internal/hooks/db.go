package hooks

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/artilugio0/proxy-vibes/internal/proxy"
	"modernc.org/sqlite" // Use the main package for error handling
)

// InitDatabase sets up the SQLite tables for requests, responses, headers, and cookies
func InitDatabase(db *sql.DB) error {
	_, err := db.Exec(`
        CREATE TABLE IF NOT EXISTS requests (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            request_id TEXT NOT NULL UNIQUE,
            method TEXT NOT NULL,
            url TEXT NOT NULL,
            body TEXT,
            timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
        );
        CREATE TABLE IF NOT EXISTS responses (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            request_id TEXT NOT NULL,
            status_code INTEGER NOT NULL,
            body TEXT,
            content_length INTEGER
        );
        CREATE TABLE IF NOT EXISTS headers (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            request_id TEXT,
            response_id TEXT,
            name TEXT NOT NULL,
            value TEXT NOT NULL,
            FOREIGN KEY (request_id) REFERENCES requests(request_id),
            FOREIGN KEY (response_id) REFERENCES responses(request_id)
        );
        CREATE TABLE IF NOT EXISTS cookies (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            request_id TEXT,
            response_id TEXT,
            name TEXT NOT NULL,
            value TEXT NOT NULL,
            FOREIGN KEY (request_id) REFERENCES requests(request_id),
            FOREIGN KEY (response_id) REFERENCES responses(request_id)
        );
        CREATE INDEX IF NOT EXISTS idx_requests_request_id ON requests (request_id);
        CREATE INDEX IF NOT EXISTS idx_responses_request_id ON responses (request_id);
        CREATE INDEX IF NOT EXISTS idx_requests_url ON requests (url);
        CREATE INDEX IF NOT EXISTS idx_responses_status_code ON responses (status_code);
        CREATE INDEX IF NOT EXISTS idx_headers_name ON headers (name);
        CREATE INDEX IF NOT EXISTS idx_headers_value ON headers (value);
        CREATE INDEX IF NOT EXISTS idx_cookies_name ON cookies (name);
        CREATE INDEX IF NOT EXISTS idx_cookies_value ON cookies (value);
    `)
	return err
}

// dbQueueItem represents an item in the database queue
type dbQueueItem struct {
	isRequest bool
	req       *http.Request
	resp      *http.Response
}

// NewDBSaveHooks returns request and response hooks that send data to a queue for asynchronous processing
func NewDBSaveHooks(db *sql.DB) (proxy.RequestInOutFunc, proxy.ResponseInOutFunc) {
	// Buffered channel to act as a queue (adjust size based on expected load)
	queue := make(chan dbQueueItem, 1000)

	// Start a goroutine to process the queue
	go func() {
		for item := range queue {
			if item.isRequest {
				err := saveRequestToDB(db, item.req)
				if err != nil {
					log.Printf("Failed to process request from queue: %v", err)
				}
			} else {
				err := saveResponseToDB(db, item.resp)
				if err != nil {
					log.Printf("Failed to process response from queue: %v", err)
				}
			}
		}
	}()

	// Request hook: sends to queue and returns immediately
	saveRequest := func(req *http.Request) error {
		id := proxy.GetRequestID(req)
		if id == "" {
			return fmt.Errorf("no request ID found")
		}

		// Send to queue (non-blocking unless queue is full)
		select {
		case queue <- dbQueueItem{isRequest: true, req: req}:
			return nil
		default:
			log.Printf("Queue full, dropping request with ID %s", id)
			return nil // Drop the request if queue is full to avoid blocking
		}
	}

	// Response hook: sends to queue and returns immediately
	saveResponse := func(resp *http.Response) error {
		id := proxy.GetResponseID(resp)
		if id == "" {
			return fmt.Errorf("no response ID found")
		}

		// Send to queue (non-blocking unless queue is full)
		select {
		case queue <- dbQueueItem{isRequest: false, resp: resp}:
			return nil
		default:
			log.Printf("Queue full, dropping response with ID %s", id)
			return nil // Drop the response if queue is full to avoid blocking
		}
	}

	return saveRequest, saveResponse
}

// saveRequestToDB performs the actual database insert for a request with retries
func saveRequestToDB(db *sql.DB, req *http.Request) error {
	id := proxy.GetRequestID(req)

	// Get body
	var body []byte
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(req.Body)
		if err != nil {
			return fmt.Errorf("failed to read request body: %v", err)
		}
		req.Body = io.NopCloser(bytes.NewBuffer(body)) // Restore body
	}

	const maxRetries = 5
	for attempt := 0; attempt < maxRetries; attempt++ {
		// Start a transaction
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %v", err)
		}

		// Insert request
		_, err = tx.Exec(`
            INSERT INTO requests (request_id, method, url, body)
            VALUES (?, ?, ?, ?)
        `, id, req.Method, req.URL.String(), string(body))
		if err == nil {
			// Insert headers
			for name, values := range req.Header {
				for _, value := range values {
					_, err = tx.Exec(`
                        INSERT INTO headers (request_id, response_id, name, value)
                        VALUES (?, NULL, ?, ?)
                    `, id, name, value)
					if err != nil {
						tx.Rollback()
						return fmt.Errorf("failed to save request header %s: %v", name, err)
					}
				}
			}

			// Insert cookies from Cookie header
			if cookies := req.Cookies(); len(cookies) > 0 {
				for _, cookie := range cookies {
					_, err = tx.Exec(`
                        INSERT INTO cookies (request_id, response_id, name, value)
                        VALUES (?, NULL, ?, ?)
                    `, id, cookie.Name, cookie.Value)
					if err != nil {
						tx.Rollback()
						return fmt.Errorf("failed to save request cookie %s: %v", cookie.Name, err)
					}
				}
			}

			// Commit if all inserts succeed
			if err = tx.Commit(); err == nil {
				return nil
			}
		}

		// Rollback on error
		tx.Rollback()

		// Check if error is due to database lock (SQLITE_BUSY)
		if sqliteErr, ok := err.(*sqlite.Error); ok && strings.Contains(strings.ToLower(sqlite.ErrorCodeString[sqliteErr.Code()]), "busy") {
			// Exponential backoff: 50ms * (2^attempt)
			delay := time.Duration(50*(1<<attempt)) * time.Millisecond
			log.Printf("Database locked for request %s, retrying in %v (attempt %d/%d)", id, delay, attempt+1, maxRetries)
			time.Sleep(delay)
			continue
		}

		// Non-retryable error, return immediately
		return fmt.Errorf("failed to save request to database: %v", err)
	}

	return fmt.Errorf("failed to save request %s after %d retries due to database lock", id, maxRetries)
}

// saveResponseToDB performs the actual database insert for a response with retries
func saveResponseToDB(db *sql.DB, resp *http.Response) error {
	id := proxy.GetResponseID(resp)

	// Get body
	var body []byte
	if resp.Body != nil {
		var err error
		body, err = io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response body: %v", err)
		}
		resp.Body = io.NopCloser(bytes.NewBuffer(body)) // Restore body
	}

	const maxRetries = 5
	for attempt := 0; attempt < maxRetries; attempt++ {
		// Start a transaction
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %v", err)
		}

		// Insert response
		_, err = tx.Exec(`
            INSERT INTO responses (request_id, status_code, body, content_length)
            VALUES (?, ?, ?, ?)
        `, id, resp.StatusCode, string(body), resp.ContentLength)
		if err == nil {
			// Insert headers
			for name, values := range resp.Header {
				for _, value := range values {
					_, err = tx.Exec(`
                        INSERT INTO headers (request_id, response_id, name, value)
                        VALUES (NULL, ?, ?, ?)
                    `, id, name, value)
					if err != nil {
						tx.Rollback()
						return fmt.Errorf("failed to save response header %s: %v", name, err)
					}
				}
			}

			// Insert cookies from Set-Cookie header
			if setCookies := resp.Header["Set-Cookie"]; len(setCookies) > 0 {
				for _, setCookie := range setCookies {
					parts := bytes.SplitN([]byte(setCookie), []byte("="), 2)
					if len(parts) == 2 {
						name := string(parts[0])
						value := string(parts[1])
						if semicolon := bytes.IndexByte([]byte(value), ';'); semicolon != -1 {
							value = value[:semicolon]
						}
						_, err = tx.Exec(`
                            INSERT INTO cookies (request_id, response_id, name, value)
                            VALUES (NULL, ?, ?, ?)
                        `, id, name, value)
						if err != nil {
							tx.Rollback()
							return fmt.Errorf("failed to save response cookie %s: %v", name, err)
						}
					}
				}
			}

			// Commit if all inserts succeed
			if err = tx.Commit(); err == nil {
				return nil
			}
		}

		// Rollback on error
		tx.Rollback()

		// Check if error is due to database lock (SQLITE_BUSY)
		if sqliteErr, ok := err.(*sqlite.Error); ok && strings.Contains(strings.ToLower(sqlite.ErrorCodeString[sqliteErr.Code()]), "busy") {
			// Exponential backoff: 50ms * (2^attempt)
			delay := time.Duration(50*(1<<attempt)) * time.Millisecond
			log.Printf("Database locked for response %s, retrying in %v (attempt %d/%d)", id, delay, attempt+1, maxRetries)
			time.Sleep(delay)
			continue
		}

		// Non-retryable error, return immediately
		return fmt.Errorf("failed to save response to database: %v", err)
	}

	return fmt.Errorf("failed to save response %s after %d retries due to database lock", id, maxRetries)
}
