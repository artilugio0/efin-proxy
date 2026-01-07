package proxyVibes

import (
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"log"
	"net/http"

	"github.com/artilugio0/efin-proxy/internal/certs"
	"github.com/artilugio0/efin-proxy/internal/grpc"
	"github.com/artilugio0/efin-proxy/internal/pipeline"
	"github.com/artilugio0/efin-proxy/internal/proxy"
)

type ProxyBuilder struct {
	CertificateFile string
	KeyFile         string

	DBFile    string
	PrintLogs bool
	SaveDir   string

	Addr     string
	GRPCAddr string

	DomainRe           string
	ExcludedExtensions []string

	RequestInHooks  []func(*http.Request) error
	RequestModHooks []func(*http.Request) (*http.Request, error)
	RequestOutHooks []func(*http.Request) error

	ResponseInHooks  []func(*http.Response) error
	ResponseModHooks []func(*http.Response) (*http.Response, error)
	ResponseOutHooks []func(*http.Response) error
}

func (pb *ProxyBuilder) GetProxy() (*Proxy, error) {
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

	excludedExtensions := defaultExcludedExtensions
	if pb.ExcludedExtensions != nil {
		excludedExtensions = append([]string{}, pb.ExcludedExtensions...)
	}
	// Initialize gRPC client manager and start the server and define gRPC hooks
	config := &proxy.Config{
		DBFile:    pb.DBFile,
		PrintLogs: pb.PrintLogs,
		SaveDir:   pb.SaveDir,

		DomainRe:           pb.DomainRe,
		ExcludedExtensions: excludedExtensions,

		RequestInHooks:  requestInHooks,
		RequestModHooks: requestModHooks,
		RequestOutHooks: requestOutHooks,

		ResponseInHooks:  responseInHooks,
		ResponseModHooks: responseModHooks,
		ResponseOutHooks: responseOutHooks,
	}

	if pb.GRPCAddr != "" {
		grpcServer := grpc.NewServer(pb.GRPCAddr, p, config)

		config.RequestInHooks = append(config.RequestInHooks, grpcServer.RequestInHook)
		config.RequestModHooks = append(config.RequestModHooks, grpcServer.RequestModHook)
		config.RequestOutHooks = append(config.RequestOutHooks, grpcServer.RequestOutHook)
		config.ResponseInHooks = append(config.ResponseInHooks, grpcServer.ResponseInHook)
		config.ResponseModHooks = append(config.ResponseModHooks, grpcServer.ResponseModHook)
		config.ResponseOutHooks = append(config.ResponseOutHooks, grpcServer.ResponseOutHook)

		go grpcServer.Run()
	}

	if err := config.Apply(p); err != nil {
		return nil, err
	}

	return &Proxy{Addr: pb.Addr, Proxy: p}, nil
}

type Proxy struct {
	Addr string
	*proxy.Proxy
}

func (p *Proxy) ListenAndServe() error {
	server := &http.Server{
		Addr: p.Addr,
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

var defaultExcludedExtensions []string = []string{
	".aac",
	".avi",
	".bin",
	".bmp",
	".dat",
	".db",
	".dll",
	".doc",
	".docx",
	".exe",
	".flac",
	".flv",
	".gif",
	".gz",
	".img",
	".iso",
	".jpeg",
	".jpg",
	".m3u8",
	".m4a",
	".m4s",
	".m4v",
	".mkv",
	".mov",
	".mp3",
	".mp4",
	".ogg",
	".pdf",
	".png",
	".rar",
	".svg",
	".tar",
	".ts",
	".wav",
	".webm",
	".webp",
	".wmv",
	".xls",
	".xlsx",
	".zip",
}
