package main

import (
	"flag"
	"log"
	"strings"

	proxyVibes "github.com/artilugio0/proxy-vibes"
	_ "modernc.org/sqlite" // SQLite driver
)

func main() {
	proxyAddr := flag.String("l", ":8080", "Proxy HTTP listen address")
	grpcAddr := flag.String("g", ":50051", "Proxy GRPC listen address")
	certFile := flag.String("cert", "", "Path to Root CA certificate file (PEM)")
	keyFile := flag.String("key", "", "Path to Root CA private key file (PEM)")
	saveDir := flag.String("d", "", "Directory to save request/response files (empty to disable)")
	dbFileShort := flag.String("D", "", "Path to SQLite database file (short form, empty to disable)")
	dbFileLong := flag.String("db-file", "", "Path to SQLite database file (long form, empty to disable)")
	printLogs := flag.Bool("p", false, "Enable raw request/response logging to stdout")
	domainRe := flag.String("s", "", "Regex specifying the domains in scope")
	excludedExtensions := flag.String("e", "", "comma separated list of excluded extensions. Example: png,jpg")
	flag.Parse()

	// Determine database path from -D or -db-file (priority: -db-file > -D)
	dbPath := *dbFileLong
	if dbPath == "" {
		dbPath = *dbFileShort
	}

	var excludedExtensionsList []string
	if *excludedExtensions != "" {
		excludedExtensionsList = strings.Split(*excludedExtensions, ",")
	}

	proxy, err := (&proxyVibes.ProxyBuilder{
		Addr:               *proxyAddr,
		GRPCAddr:           *grpcAddr,
		CertificateFile:    *certFile,
		KeyFile:            *keyFile,
		DBFile:             dbPath,
		PrintLogs:          *printLogs,
		SaveDir:            *saveDir,
		DomainRe:           *domainRe,
		ExcludedExtensions: excludedExtensionsList,
	}).GetProxy()

	if err != nil {
		panic(err)
	}

	log.Printf("Starting HTTP proxy server on %s", *proxyAddr)
	log.Fatal(proxy.ListenAndServe())
}
