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

	"github.com/artilugio0/proxy-vibes/internal/ids"
	"github.com/artilugio0/proxy-vibes/internal/pipeline"
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
            response_id TEXT NOT NULL,
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
            FOREIGN KEY (response_id) REFERENCES responses(response_id)
        );
        CREATE TABLE IF NOT EXISTS cookies (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            request_id TEXT,
            response_id TEXT,
            name TEXT NOT NULL,
            value TEXT NOT NULL,
            FOREIGN KEY (request_id) REFERENCES requests(request_id),
            FOREIGN KEY (response_id) REFERENCES responses(response_id)
        );
        CREATE INDEX IF NOT EXISTS idx_requests_request_id ON requests (request_id);
        CREATE INDEX IF NOT EXISTS idx_responses_request_id ON responses (response_id);
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
func NewDBSaveHooks(dbFile string) (pipeline.ReadOnlyHook[*http.Request], pipeline.ReadOnlyHook[*http.Response]) {
	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		log.Printf("Failed to open SQLite database: %v", err)
	}

	err = InitDatabase(db)
	if err != nil {
		log.Printf("Failed to initialize database: %v", err)
	}
	db.Close()

	// Buffered channel to act as a queue (adjust size based on expected load)
	queue := make(chan dbQueueItem, 1000)

	// Start a goroutine to process the queue
	go func() {
		for item := range queue {
			if item.isRequest {
				err := saveRequestToDB(dbFile, item.req)
				if err != nil {
					log.Printf("Failed to process request from queue: %v", err)
				}
			} else {
				err := saveResponseToDB(dbFile, item.resp)
				if err != nil {
					log.Printf("Failed to process response from queue: %v", err)
				}
			}

			db.Close()
		}
	}()

	// Request hook: sends to queue and returns immediately
	saveRequest := func(req *http.Request) error {
		id := ids.GetRequestID(req)
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
		id := ids.GetResponseID(resp)
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
func saveRequestToDB(dbFile string, req *http.Request) error {
	id := ids.GetRequestID(req)

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

	err := retry(maxRetries, func() (bool, error) {
		db, err := sql.Open("sqlite", dbFile)
		if err != nil {
			log.Printf("Failed to open SQLite database: %v", err)
			return false, err
		}
		defer db.Close()

		err = func() error {
			// Start a transaction
			tx, err := db.Begin()
			if err != nil {
				return err
			}
			defer tx.Rollback()

			// Insert request
			_, err = tx.Exec(`
				INSERT INTO requests (request_id, method, url, body)
				VALUES (?, ?, ?, ?)
			`, id, req.Method, req.URL.String(), string(body))
			if err == nil {
				// Insert headers, including Host if present
				for name, values := range req.Header {
					for _, value := range values {
						_, err = tx.Exec(`
                        INSERT INTO headers (request_id, response_id, name, value)
                        VALUES (?, NULL, ?, ?)
                    `, id, name, value)
						if err != nil {
							return err
						}
					}
				}

				// Explicitly save the Host header if it’s set and not already in Header map
				if req.Host != "" && req.Header.Get("Host") == "" {
					_, err = tx.Exec(`
                    INSERT INTO headers (request_id, response_id, name, value)
                    VALUES (?, NULL, ?, ?)
                `, id, "Host", req.Host)
					if err != nil {
						return err
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
							return err
						}
					}
				}
			}

			if err := tx.Commit(); err != nil {
				log.Printf("commit error: %v", err)
				return err
			}

			return nil
		}()

		if err != nil {
			// Check if error is due to database lock (SQLITE_BUSY)
			if sqliteErr, ok := err.(*sqlite.Error); ok && strings.Contains(strings.ToLower(sqlite.ErrorCodeString[sqliteErr.Code()]), "busy") {
				log.Printf("Database locked for request %s, retrying...: %v", id, err)
				return true, err
			}

			return false, err
		}

		return false, nil
	})

	if err == nil {
		return nil
	}

	return fmt.Errorf("failed to save request to database: %v", err)
}

// saveResponseToDB performs the actual database insert for a response with retries
func saveResponseToDB(dbFile string, resp *http.Response) error {
	id := ids.GetResponseID(resp)

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
	err := retry(maxRetries, func() (bool, error) {
		db, err := sql.Open("sqlite", dbFile)
		if err != nil {
			log.Printf("Failed to open SQLite database: %v", err)
			return false, err
		}
		defer db.Close()

		err = func() error {
			// Start a transaction
			tx, err := db.Begin()
			if err != nil {
				return err
			}
			defer tx.Rollback()

			contentLength := resp.ContentLength
			if contentLength == -1 {
				contentLength = int64(len(body))
			}

			// Insert response
			_, err = tx.Exec(`
				INSERT INTO responses (response_id, status_code, body, content_length)
				VALUES (?, ?, ?, ?)
			`, id, resp.StatusCode, string(body), contentLength)
			if err == nil {
				// Insert headers
				for name, values := range resp.Header {
					for _, value := range values {
						_, err = tx.Exec(`
							INSERT INTO headers (request_id, response_id, name, value)
							VALUES (NULL, ?, ?, ?)
						`, id, name, value)
						if err != nil {
							return err
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
								return err
							}
						}
					}
				}
			}

			if err := tx.Commit(); err != nil {
				log.Printf("commit error: %v", err)
				return err
			}

			return nil
		}()

		if err != nil {
			// Check if error is due to database lock (SQLITE_BUSY)
			if sqliteErr, ok := err.(*sqlite.Error); ok && strings.Contains(strings.ToLower(sqlite.ErrorCodeString[sqliteErr.Code()]), "busy") {
				log.Printf("Database locked for response %s, retrying...: %v", id, err)
				return true, err
			}

			return false, err
		}

		return false, nil

	})

	if err == nil {
		return nil
	}

	return fmt.Errorf("failed to save response to database: %v", err)
}

func retry(attempts int, f func() (bool, error)) error {
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		var attemptRetry bool
		attemptRetry, err = f()
		if err == nil {
			return nil
		}

		if !attemptRetry {
			return err
		}

		delay := time.Duration(500*(1<<attempt)) * time.Millisecond
		time.Sleep(delay)
	}

	return err
}
