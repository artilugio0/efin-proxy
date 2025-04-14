package proxyVibes

import (
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"path"
	"strings"

	"github.com/artilugio0/proxy-vibes/internal/certs"
	"github.com/artilugio0/proxy-vibes/internal/grpc"
	"github.com/artilugio0/proxy-vibes/internal/hooks"
	"github.com/artilugio0/proxy-vibes/internal/pipeline"
	"github.com/artilugio0/proxy-vibes/internal/proxy"
)

type ProxyBuilder struct {
	CertificateFile string
	KeyFile         string

	DBFile    string
	PrintLogs bool
	SaveDir   string

	Addr     string
	GRPCAddr string

	RequestInHooks  []func(*http.Request) error
	RequestModHooks []func(*http.Request) (*http.Request, error)
	RequestOutHooks []func(*http.Request) error

	ResponseInHooks  []func(*http.Response) error
	ResponseModHooks []func(*http.Response) (*http.Response, error)
	ResponseOutHooks []func(*http.Response) error
}

func (pb *ProxyBuilder) GetProxy() (*Proxy, error) {
	var db *sql.DB
	var err error

	if pb.DBFile != "" {
		db, err = sql.Open("sqlite", pb.DBFile)
		if err != nil {
			return nil, fmt.Errorf("Failed to open SQLite database: %v", err)
		}
		defer db.Close()

		err = hooks.InitDatabase(db)
		if err != nil {
			return nil, fmt.Errorf("Failed to initialize database: %v", err)
		}
	}

	var rootCA *x509.Certificate
	var rootKey *rsa.PrivateKey

	if pb.CertificateFile != "" && pb.KeyFile != "" {
		rca, rk, err := certs.LoadRootCA(pb.CertificateFile, pb.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("Error loading Root CA from %s and %s: %v",
				pb.CertificateFile, pb.KeyFile, err)
		}
		rootCA = rca
		rootKey = rk
		log.Printf("Loaded Root CA from %s and %s", pb.CertificateFile, pb.KeyFile)
	} else {
		rca, rk, rootCAPEM, rootKeyPEM, err := certs.GenerateRootCA()
		if err != nil {
			return nil, fmt.Errorf("Error generating Root CA: %v", err)
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

	requestInHooks := []pipeline.ReadOnlyHook[*http.Request]{}
	for _, h := range pb.RequestInHooks {
		requestInHooks = append(requestInHooks, h)
	}
	requestModHooks := []pipeline.ModHook[*http.Request]{}
	for _, h := range pb.RequestModHooks {
		requestModHooks = append(requestModHooks, h)
	}
	requestOutHooks := []pipeline.ReadOnlyHook[*http.Request]{}
	for _, h := range pb.RequestOutHooks {
		requestOutHooks = append(requestOutHooks, h)
	}
	responseInHooks := []pipeline.ReadOnlyHook[*http.Response]{}
	for _, h := range pb.ResponseInHooks {
		responseInHooks = append(responseInHooks, h)
	}
	responseModHooks := []pipeline.ModHook[*http.Response]{}
	for _, h := range pb.ResponseModHooks {
		responseModHooks = append(responseModHooks, h)
	}
	responseOutHooks := []pipeline.ReadOnlyHook[*http.Response]{}
	for _, h := range pb.ResponseOutHooks {
		responseOutHooks = append(responseOutHooks, h)
	}

	// Add logging hooks if -p is set
	if pb.PrintLogs {
		requestOutHooks = append(requestOutHooks, hooks.LogRawRequest)
		responseInHooks = append(responseInHooks, hooks.LogRawResponse)
		log.Printf("Enabled raw request/response logging to stdout")
	}

	// Add Accept-Encoding removal hook
	requestModHooks = append(requestModHooks, func(r *http.Request) (*http.Request, error) {
		r.Header.Del("Accept-Encoding")
		return r, nil
	})

	// Add database save hooks if database is initialized
	if db != nil {
		saveRequest, saveResponse := hooks.NewDBSaveHooks(db)
		requestOutHooks = append(requestOutHooks, saveRequest)
		responseInHooks = append(responseInHooks, saveResponse)
		log.Printf("Saving requests and responses to database at %s", pb.DBFile)
	}

	// Add file save hooks if directory is specified
	if pb.SaveDir != "" {
		saveRequest, saveResponse := hooks.NewFileSaveHooks(pb.SaveDir)
		requestOutHooks = append(requestOutHooks, saveRequest)
		responseInHooks = append(responseInHooks, saveResponse)
		log.Printf("Saving requests and responses to directory: %s", pb.SaveDir)
	}

	// Initialize gRPC client manager and start the server and define gRPC hooks
	grpcServer := grpc.NewServer()

	requestInHooks = append(requestInHooks, grpcServer.RequestInHook)
	requestModHooks = append(requestModHooks, grpcServer.RequestModHook)
	requestOutHooks = append(requestOutHooks, grpcServer.RequestOutHook)
	responseInHooks = append(responseInHooks, grpcServer.ResponseInHook)
	responseModHooks = append(responseModHooks, grpcServer.ResponseModHook)
	responseOutHooks = append(responseOutHooks, grpcServer.ResponseOutHook)

	p.SetScope(isInScope)

	p.SetRequestInHooks(requestInHooks)
	p.SetRequestModHooks(requestModHooks)
	p.SetRequestOutHooks(requestOutHooks)
	p.SetResponseInHooks(responseInHooks)
	p.SetResponseModHooks(responseModHooks)
	p.SetResponseOutHooks(responseOutHooks)

	return &Proxy{Proxy: p}, nil
}

type Proxy struct {
	Addr string
	*proxy.Proxy
}

func (p *Proxy) ListenAndServe() error {
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

	return server.ListenAndServe()
}

// isInScope and other helper functions remain unchanged...
func isInScope(r *http.Request) bool {
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
		".img":  true,
	}
	return binaryExtensions[ext] && !multimediaExtensions[ext]
}
