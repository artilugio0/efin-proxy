package efinproxy

import (
	"database/sql"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/artilugio0/efincore"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

const maxRetries int = 10

type Sqlite struct {
	db *sql.DB

	// writeMux helps to avoid multiple goroutines competing for write access
	// which makes batches of request to be processed faster by avoiding sqlite3 db locks
	writeMux *sync.Mutex
}

func NewSqlite(dbFile string) (*Sqlite, error) {
	fileExists := true
	stat, err := os.Stat(dbFile)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		fileExists = false
	}

	if fileExists && stat.IsDir() {
		return nil, fmt.Errorf("file '%s' is a directory, not an sqlite3 file", dbFile)
	}

	db, err := sql.Open("sqlite3", dbFile)
	if err != nil {
		return nil, err
	}

	result := &Sqlite{
		db:       db,
		writeMux: &sync.Mutex{},
	}

	if err := result.runMigrations(); err != nil {
		return nil, err
	}

	return result, nil
}

func (s *Sqlite) SaveResponse(r *http.Response, id uuid.UUID) error {
	query := `
		INSERT INTO responses (id, version, status, status_code, body) VALUES (?, ?, ?, ?, ?);
	`
	body, err := r.Body.(*efincore.RBody).GetBytes()
	if err != nil {
		return err
	}

	err = withRetries(func() error {
		s.writeMux.Lock()
		defer s.writeMux.Unlock()
		_, err := s.db.Exec(
			query,
			id.String(),
			r.Proto,
			http.StatusText(r.StatusCode),
			r.StatusCode,
			body,
		)

		return err
	})

	if err := s.saveHeaders(id.String(), "responses", r.Header); err != nil {
		return err
	}

	return s.saveCookies(id.String(), "responses", r.Cookies())
}

func (s *Sqlite) SaveRequest(r *http.Request, id uuid.UUID) error {
	query := `
		INSERT INTO requests (id, method, url, version, body, timestamp, tag) VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP, ?);
	`

	body, err := r.Body.(*efincore.RBody).GetBytes()
	if err != nil {
		return err
	}

	err = withRetries(func() error {
		s.writeMux.Lock()
		defer s.writeMux.Unlock()
		_, err = s.db.Exec(
			query,
			id.String(),
			r.Method,
			r.URL.String(),
			r.Proto,
			body,
			"",
		)

		return err
	})

	if err != nil {
		return err
	}

	if err := s.saveHeaders(id.String(), "requests", r.Header); err != nil {
		return err
	}

	return s.saveCookies(id.String(), "requests", r.Cookies())
}

func (s *Sqlite) saveHeaders(id string, table string, header http.Header) error {
	var query string
	if table == "requests" {
		query = "INSERT INTO headers (request_id, name, value) VALUES (?, ?, ?);"
	} else {
		query = "INSERT INTO headers (response_id, name, value) VALUES (?, ?, ?);"
	}

	for h, vs := range header {
		for _, v := range vs {
			err := withRetries(func() error {
				s.writeMux.Lock()
				defer s.writeMux.Unlock()
				_, err := s.db.Exec(query, id, strings.ToLower(h), v)

				return err
			})

			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *Sqlite) saveCookies(id string, table string, cookies []*http.Cookie) error {
	var query string
	if table == "requests" {
		query = "INSERT INTO cookies (request_id, name, value, path, domain, expires, max_age, same_site, secure, http_only, partitioned) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
	} else {
		query = "INSERT INTO cookies (response_id, name, value, path, domain, expires, max_age, same_site, secure, http_only, partitioned) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
	}

	for _, c := range cookies {
		var sameSite string = "lax"

		switch c.SameSite {
		case http.SameSiteDefaultMode:
			sameSite = "lax"
		case http.SameSiteLaxMode:
			sameSite = "lax"
		case http.SameSiteNoneMode:
			sameSite = "none"
		case http.SameSiteStrictMode:
			sameSite = "strict"
		}

		err := withRetries(func() error {
			s.writeMux.Lock()
			defer s.writeMux.Unlock()
			_, err := s.db.Exec(
				query,
				id,
				c.Name,
				c.Value,
				c.Path,
				c.Domain,
				c.Expires.Format(time.RFC3339),
				c.MaxAge,
				sameSite,
				c.Secure,
				c.HttpOnly,
				false,
			)

			return err
		})

		if err != nil {
			return err
		}
	}

	return nil
}

const schemaVersion int = 2

var migrations [](func(*Sqlite) error) = []func(*Sqlite) error{
	version0Migration,
	version1Migration,
	version2CreateIndeces,
}

func (s *Sqlite) runMigrations() error {
	currentSchemaVersion := -1

	checkRequestTable := s.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='requests';")
	var n string
	if err := checkRequestTable.Scan(&n); err == nil {
		currentSchemaVersion = 0

		row := s.db.QueryRow("SELECT max(version) FROM schema_version")
		if err := row.Scan(&currentSchemaVersion); err != nil {
			// if error is that schema_version does not exist, we need to run all migrations
			// with any other error the migrations need to be aborted
			if !strings.Contains(err.Error(), "no such table") {
				return err
			}
		}
	}

	if currentSchemaVersion >= len(migrations) {
		return nil
	}

	for i := currentSchemaVersion + 1; i < len(migrations); i++ {
		if err := migrations[i](s); err != nil {
			return err
		}
	}

	return nil
}

func version0Migration(s *Sqlite) error {
	statements := []string{
		`CREATE TABLE requests (id VARCHAR(64), method VARCHAR(16), url VARCHAR(1024), version VARCHAR(16), body BLOB, timestamp TEXT, tag VARCHAR(128));`,
		`CREATE TABLE responses (id VARCHAR(64), status_code int, status VARCHAR(16), version VARCHAR(16), body BLOB);`,
		`CREATE TABLE headers (request_id VARCHAR(64), response_id VARCHAR(64), name VARCHAR(128), value VARCHAR(1024));`,
		`CREATE TABLE cookies (
            request_id VARCHAR(64),
            response_id VARCHAR(64),
            name VARCHAR(128),
            value VARCHAR(1024),
            path VARCHAR(128),
            domain VARCHAR(128),
            expires VARCHAR(128),
            max_age VARCHAR(128),
            same_site VARCHAR(32),
            secure INT,
            http_only INT,
            partitioned INT
        );`,
	}

	for _, statement := range statements {
		err := withRetries(func() error {
			s.writeMux.Lock()
			defer s.writeMux.Unlock()
			_, err := s.db.Exec(statement)

			return err
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func version1Migration(s *Sqlite) error {
	statements := []string{
		`CREATE TABLE schema_version (version INT);`,
		`INSERT INTO schema_version (version) VALUES (` + strconv.Itoa(schemaVersion) + `);`,
		`CREATE TABLE wsframes (
            request_id VARCHAR(64),
            response_id VARCHAR(64),
            timestamp TEXT,
            fin INT,
            rsv INT,
            mask INT,
            opcode INT,
            length INT,
            masking_key VARCHAR(4),
            body BLOB
        );`,
	}

	for _, statement := range statements {
		err := withRetries(func() error {
			s.writeMux.Lock()
			defer s.writeMux.Unlock()
			_, err := s.db.Exec(statement)

			return err
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func version2CreateIndeces(s *Sqlite) error {
	statements := []string{
		`CREATE INDEX headers_request_id_index ON headers (request_id);`,
		`CREATE INDEX headers_response_id_index ON headers (response_id);`,
		`CREATE INDEX requests_id_index ON requests (id);`,
		`CREATE INDEX responses_id_index ON responses (id);`,
		`CREATE INDEX requests_timestamp_index ON requests (timestamp);`,
		`CREATE INDEX requests_url_index ON requests (url);`,
		`CREATE INDEX wsframes_request_id_index on wsframes (request_id);`,
		`CREATE INDEX wsframes_response_id_index on wsframes (response_id);`,
		`INSERT INTO schema_version (version) VALUES (` + strconv.Itoa(schemaVersion) + `);`,
	}

	for _, statement := range statements {
		err := withRetries(func() error {
			s.writeMux.Lock()
			defer s.writeMux.Unlock()
			_, err := s.db.Exec(statement)

			return err
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func withRetries(f func() error) error {
	for i := 0; i < maxRetries; i++ {
		err := f()

		if err != nil {
			if i == maxRetries-1 {
				return err
			}

			time.Sleep(time.Duration(50+(rand.Int()%50)) * time.Millisecond)

			continue
		}

		break
	}

	return nil
}
