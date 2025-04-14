package main

import (
	"flag"
	"log"

	proxyVibes "github.com/artilugio0/proxy-vibes"
	_ "modernc.org/sqlite" // SQLite driver
)

func main() {
	certFile := flag.String("cert", "", "Path to Root CA certificate file (PEM)")
	keyFile := flag.String("key", "", "Path to Root CA private key file (PEM)")
	saveDir := flag.String("d", "", "Directory to save request/response files (empty to disable)")
	dbFileShort := flag.String("D", "", "Path to SQLite database file (short form, empty to disable)")
	dbFileLong := flag.String("db-file", "", "Path to SQLite database file (long form, empty to disable)")
	printLogs := flag.Bool("p", false, "Enable raw request/response logging to stdout")
	flag.Parse()

	// Determine database path from -D or -db-file (priority: -db-file > -D)
	dbPath := *dbFileLong
	if dbPath == "" {
		dbPath = *dbFileShort
	}

	proxy, err := (&proxyVibes.ProxyBuilder{
		CertificateFile: *certFile,
		KeyFile:         *keyFile,
		DBFile:          dbPath,
		PrintLogs:       *printLogs,
		SaveDir:         *saveDir,
	}).GetProxy()

	if err != nil {
		panic(err)
	}

	log.Printf("Starting HTTP proxy server on :8080")
	log.Fatal(proxy.ListenAndServe())
}
