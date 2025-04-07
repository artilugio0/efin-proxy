package main

import (
	"crypto/rsa"
	"crypto/x509"
	"flag"
	"fmt"
	"log"
	"net/http"
	"path"
	"strings"

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

	p.RequestOutPipeline = append(p.RequestOutPipeline, hooks.LogRawRequest)
	p.RequestModPipeline = append(p.RequestModPipeline, func(r *http.Request) (*http.Request, error) {
		r.Header.Del("Accept-Encoding")
		return r, nil
	})
	p.ResponseInPipeline = append(p.ResponseInPipeline, hooks.LogRawResponse)
	// Only add save hooks if the d flag is specified and not empty
	if *saveDir != "" {
		saveRequest, saveResponse := hooks.NewFileSaveHooks(*saveDir)
		p.RequestOutPipeline = append(p.RequestOutPipeline, saveRequest)
		p.ResponseInPipeline = append(p.ResponseInPipeline, saveResponse)
		log.Printf("Saving requests and responses to directory: %s", *saveDir)
	}
	p.InScopeFunc = IsInScope

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

func IsInScope(r *http.Request) bool {
	return !IsMultimediaRequest(r) && !IsBinaryDataRequest(r)
}

// IsMultimediaRequest checks if the request is for a multimedia resource
// (image, video, or audio) based on the URL's file extension
func IsMultimediaRequest(r *http.Request) bool {
	// Get the path from the request URL
	urlPath := r.URL.Path

	// Extract the file extension (converted to lowercase for consistency)
	ext := strings.ToLower(path.Ext(urlPath))

	// If there's no extension, it's not a multimedia file
	if ext == "" {
		return false
	}

	// Lists of common multimedia file extensions
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

	// Check if the extension matches any multimedia type
	return imageExtensions[ext] || videoExtensions[ext] || audioExtensions[ext]
}

// IsBinaryDataRequest checks if the request is for a binary data resource
// (excluding multimedia types like images, videos, and audio)
func IsBinaryDataRequest(r *http.Request) bool {
	// Get the path from the request URL
	urlPath := r.URL.Path

	// Extract the file extension (converted to lowercase for consistency)
	ext := strings.ToLower(path.Ext(urlPath))

	// If there's no extension, assume it's not a binary file
	if ext == "" {
		return false
	}

	// Define multimedia extensions to exclude them
	multimediaExtensions := map[string]bool{
		// Image extensions
		".jpg":  true,
		".jpeg": true,
		".png":  true,
		".gif":  true,
		".bmp":  true,
		".webp": true,
		".svg":  true,
		// Video extensions
		".mp4":  true,
		".m4v":  true,
		".mov":  true,
		".avi":  true,
		".wmv":  true,
		".flv":  true,
		".webm": true,
		".mkv":  true,
		// Audio extensions
		".mp3":  true,
		".m4a":  true,
		".wav":  true,
		".ogg":  true,
		".flac": true,
		".aac":  true,
	}

	// Define common binary data extensions (non-multimedia)
	binaryExtensions := map[string]bool{
		".pdf":  true, // PDF documents
		".doc":  true, // Word documents (older binary format)
		".docx": true, // Word documents (XML-based but often treated as binary)
		".xls":  true, // Excel spreadsheets (older binary format)
		".xlsx": true, // Excel spreadsheets
		".zip":  true, // Zip archives
		".rar":  true, // RAR archives
		".tar":  true, // Tar archives
		".gz":   true, // Gzip files
		".exe":  true, // Windows executables
		".dll":  true, // Dynamic link libraries
		".bin":  true, // Generic binary files
		".iso":  true, // Disk images
		".dat":  true, // Generic data files
		".db":   true, // Database files
	}

	// Check if it's a binary extension but not a multimedia one
	return binaryExtensions[ext] && !multimediaExtensions[ext]
}
