package main

import (
	"crypto/rsa"
	"crypto/x509"
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/artilugio0/proxy-vibes/internal/certs"
	"github.com/artilugio0/proxy-vibes/internal/hooks"
	"github.com/artilugio0/proxy-vibes/internal/proxy"
)

func main() {
	certFile := flag.String("cert", "", "Path to Root CA certificate file (PEM)")
	keyFile := flag.String("key", "", "Path to Root CA private key file (PEM)")
	saveDir := flag.String("d", "", "Directory to save request/response files (empty to disable)")
	flag.Parse()

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
	p.RequestInPipeline = append(p.RequestInPipeline, hooks.LogRawRequest)
	p.RequestModPipeline = append(p.RequestModPipeline, func(req *http.Request) (*http.Request, error) {
		req.Header.Set("X-Modified", "true")
		return req, nil
	})
	p.ResponseInPipeline = append(p.ResponseInPipeline, hooks.LogRawResponse)

	// Only add save hooks if the d flag is specified and not empty
	if *saveDir != "" {
		saveRequest, saveResponse := hooks.NewFileSaveHooks(*saveDir)
		p.RequestInPipeline = append(p.RequestInPipeline, saveRequest)
		p.ResponseInPipeline = append(p.ResponseInPipeline, saveResponse)
		log.Printf("Saving requests and responses to directory: %s", *saveDir)
	}

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
