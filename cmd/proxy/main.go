package main

import (
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"net/http"
	"path"
	"strings"

	_ "modernc.org/sqlite" // SQLite driver

	"github.com/artilugio0/proxy-vibes/internal/certs"
	"github.com/artilugio0/proxy-vibes/internal/hooks"
	"github.com/artilugio0/proxy-vibes/internal/proxy"
)

func main() {
	certFile := flag.String("cert", "", "Path to Root CA certificate file (PEM)")
	keyFile := flag.String("key", "", "Path to Root CA private key file (PEM)")
	saveDir := flag.String("d", "", "Directory to save request/response files (empty to disable)")
	dbFileShort := flag.String("D", "", "Path to SQLite database file (short form, empty to disable)")
	dbFileLong := flag.String("db-file", "", "Path to SQLite database file (long form, empty to disable)")
	printLogs := flag.Bool("p", false, "Enable raw request/response logging to stdout")
	flag.Parse()

	var db *sql.DB
	var err error

	// Determine database path from -D or -db-file (priority: -db-file > -D)
	dbPath := *dbFileLong
	if dbPath == "" {
		dbPath = *dbFileShort
	}

	// Only initialize database if -D or -db-file is set
	if dbPath != "" {
		db, err = sql.Open("sqlite", dbPath)
		if err != nil {
			log.Fatalf("Failed to open SQLite database: %v", err)
		}
		defer db.Close()

		// Create tables if they don’t exist
		err = hooks.InitDatabase(db)
		if err != nil {
			log.Fatalf("Failed to initialize database: %v", err)
		}
	}

	var rootCA *x509.Certificate
	var rootKey *rsa.PrivateKey

	// Use provided cert and key files if both are specified, otherwise generate a new Root CA
	if *certFile != "" && *keyFile != "" {
		rca, rk, err := certs.LoadRootCA(*certFile, *keyFile)
		if err != nil {
			log.Fatalf("Error loading Root CA from %s and %s: %v", *certFile, *keyFile, err)
		}
		rootCA = rca
		rootKey = rk
		log.Printf("Loaded Root CA from %s and %s", *certFile, *keyFile)
	} else {
		rca, rk, rootCAPEM, rootKeyPEM, err := certs.GenerateRootCA()
		if err != nil {
			log.Fatalf("Error generating Root CA: %v", err)
		}
		rootCA = rca
		rootKey = rk
		fmt.Println("Generated new Root CA:")
		fmt.Println("=== Proxy Root CA Certificate (Save this to a .crt file) ===")
		fmt.Println(rootCAPEM)
		fmt.Println("=== End of Certificate ===")
		fmt.Println("=== Proxy Root CA Private Key (Save this to a .key file) ===")
		fmt.Println(rootKeyPEM)
		fmt.Println("=== End of Private Key ===")
	}

	p := proxy.NewProxy(rootCA, rootKey)

	requestInHooks := []proxy.ReadOnlyHook[*http.Request]{}
	requestModHooks := []proxy.ModHook[*http.Request]{}
	requestOutHooks := []proxy.ReadOnlyHook[*http.Request]{}
	responseInHooks := []proxy.ReadOnlyHook[*http.Response]{}
	responseModHooks := []proxy.ModHook[*http.Response]{}
	responseOutHooks := []proxy.ReadOnlyHook[*http.Response]{}

	// Add logging hooks only if -p is set
	if *printLogs {
		requestOutHooks = append(requestOutHooks, hooks.LogRawRequest)
		responseInHooks = append(responseInHooks, hooks.LogRawResponse)
		log.Printf("Enabled raw request/response logging to stdout")
	}

	// Always add the Accept-Encoding removal hook (independent of flags)
	requestModHooks = append(requestModHooks, func(r *http.Request) (*http.Request, error) {
		r.Header.Del("Accept-Encoding")
		return r, nil
	})

	// Add database save hooks only if database is initialized (-D or -db-file)
	if db != nil {
		saveRequest, saveResponse := hooks.NewDBSaveHooks(db)
		requestOutHooks = append(requestOutHooks, saveRequest)
		responseInHooks = append(responseInHooks, saveResponse)
		log.Printf("Saving requests and responses to database at %s", dbPath)
	}

	// Add file save hooks if directory is specified (-d)
	if *saveDir != "" {
		saveRequest, saveResponse := hooks.NewFileSaveHooks(*saveDir)
		requestOutHooks = append(requestOutHooks, saveRequest)
		responseInHooks = append(responseInHooks, saveResponse)
		log.Printf("Saving requests and responses to directory: %s", *saveDir)
	}
	p.InScopeFunc = IsInScope

	p.SetRequestInHooks(requestInHooks)
	p.SetRequestModHooks(requestModHooks)
	p.SetRequestOutHooks(requestOutHooks)
	p.SetResponseInHooks(responseInHooks)
	p.SetResponseModHooks(responseModHooks)
	p.SetResponseOutHooks(responseOutHooks)

	server := &http.Server{
		Addr: ":8080",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodConnect {
				p.HandleConnect(w, r)
			} else {
				p.ServeHTTP(w, r)
			}
		}),
	}

	log.Printf("Starting HTTP proxy server on :8080")
	log.Fatal(server.ListenAndServe())
}

// IsInScope and other helper functions remain unchanged...
func IsInScope(r *http.Request) bool {
	return !IsMultimediaRequest(r) && !IsBinaryDataRequest(r)
}

func IsMultimediaRequest(r *http.Request) bool {
	urlPath := r.URL.Path
	ext := strings.ToLower(path.Ext(urlPath))
	if ext == "" {
		return false
	}
	imageExtensions := map[string]bool{
		".jpg":  true,
		".jpeg": true,
		".png":  true,
		".gif":  true,
		".bmp":  true,
		".webp": true,
		".svg":  true,
	}
	videoExtensions := map[string]bool{
		".mp4":  true,
		".m4v":  true,
		".mov":  true,
		".avi":  true,
		".wmv":  true,
		".flv":  true,
		".webm": true,
		".mkv":  true,
	}
	audioExtensions := map[string]bool{
		".mp3":  true,
		".m4a":  true,
		".wav":  true,
		".ogg":  true,
		".flac": true,
		".aac":  true,
	}
	return imageExtensions[ext] || videoExtensions[ext] || audioExtensions[ext]
}

func IsBinaryDataRequest(r *http.Request) bool {
	urlPath := r.URL.Path
	ext := strings.ToLower(path.Ext(urlPath))
	if ext == "" {
		return false
	}
	multimediaExtensions := map[string]bool{
		".jpg":  true,
		".jpeg": true,
		".png":  true,
		".gif":  true,
		".bmp":  true,
		".webp": true,
		".svg":  true,
		".mp4":  true,
		".m4v":  true,
		".mov":  true,
		".avi":  true,
		".wmv":  true,
		".flv":  true,
		".webm": true,
		".mkv":  true,
		".mp3":  true,
		".m4a":  true,
		".wav":  true,
		".ogg":  true,
		".flac": true,
		".aac":  true,
	}
	binaryExtensions := map[string]bool{
		".pdf":  true,
		".doc":  true,
		".docx": true,
		".xls":  true,
		".xlsx": true,
		".zip":  true,
		".rar":  true,
		".tar":  true,
		".gz":   true,
		".exe":  true,
		".dll":  true,
		".bin":  true,
		".iso":  true,
		".dat":  true,
		".db":   true,
	}
	return binaryExtensions[ext] && !multimediaExtensions[ext]
}
