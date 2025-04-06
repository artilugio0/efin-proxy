package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/artilugio0/proxy-vibes/internal/certs"
	"github.com/artilugio0/proxy-vibes/internal/proxy"
)

func main() {
	certFile := flag.String("cert", "", "Path to Root CA certificate file (PEM)")
	keyFile := flag.String("key", "", "Path to Root CA private key file (PEM)")
	flag.Parse()

	var rootCA, rootKey, rootCAPEM, rootKeyPEM, err = certs.GenerateRootCA()
	if *certFile != "" && *keyFile != "" {
		rootCA, rootKey, err = certs.LoadRootCA(*certFile, *keyFile)
		if err != nil {
			log.Fatalf("Error loading Root CA: %v", err)
		}
		certPEM, _ := os.ReadFile(*certFile)
		rootCAPEM = string(certPEM)
		log.Printf("Loaded Root CA from %s and %s", *certFile, *keyFile)
	} else {
		rootCA, rootKey, rootCAPEM, rootKeyPEM, err = certs.GenerateRootCA()
		if err != nil {
			log.Fatalf("Error generating Root CA: %v", err)
		}
		fmt.Println("=== Proxy Root CA Certificate (Save this to a .crt file) ===")
		fmt.Println(rootCAPEM)
		fmt.Println("=== End of Certificate ===")
		fmt.Println("=== Proxy Root CA Private Key (Save this to a .key file) ===")
		fmt.Println(rootKeyPEM)
		fmt.Println("=== End of Private Key ===")
	}

	p := proxy.NewProxy(rootCA, rootKey)
	p.RequestModPipeline = append(p.RequestModPipeline, func(req *http.Request) (*http.Request, error) {
		req.Header.Set("X-Modified", "true")
		return req, nil
	})

	server := &http.Server{
		Addr: ":8080",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodConnect {
				p.HandleConnect(w, r) // Call as method on p
			} else {
				p.ServeHTTP(w, r)
			}
		}),
	}

	log.Printf("Starting HTTP proxy server on :8080")
	log.Fatal(server.ListenAndServe())
}
