package cmd

import (
	"log"
	"os"
	"strings"

	proxyVibes "github.com/artilugio0/proxy-vibes"
	"github.com/spf13/cobra"
)

const (
	DefaultAddr              string = "127.0.0.1:8669"
	DefaultCertFile          string = ""
	DefaultDBFile            string = ""
	DefaultExcludeExtensions string = "png|gif|jpeg|jpg|aac|ts"
	DefaultGRPCAddr          string = "127.0.0.1:8670"
	DefaultKeyFile           string = ""
	DefaultPrint             bool   = false
	DefaultSaveDir           string = ""
	DefaultScope             string = ".*"
)

// Execute runs the root command.
func Execute() {
	if err := NewProxyCmd("efin-proxy").Execute(); err != nil {
		os.Exit(1)
	}
}

func NewProxyCmd(use string) *cobra.Command {
	var (
		proxyAddr          string
		grpcAddr           string
		certFile           string
		keyFile            string
		saveDir            string
		dbFile             string
		printLogs          bool
		domainRe           string
		excludedExtensions string
	)

	efinProxyCmd := &cobra.Command{
		Use:   use,
		Short: "Run HTTP Interceptor Proxy",
		Run: func(cmd *cobra.Command, args []string) {
			var excludedExtensionsList []string
			if excludedExtensions != "" {
				excludedExtensionsList = strings.Split(excludedExtensions, ",")
			}

			proxy, err := (&proxyVibes.ProxyBuilder{
				Addr:               proxyAddr,
				GRPCAddr:           grpcAddr,
				CertificateFile:    certFile,
				KeyFile:            keyFile,
				DBFile:             dbFile,
				PrintLogs:          printLogs,
				SaveDir:            saveDir,
				DomainRe:           domainRe,
				ExcludedExtensions: excludedExtensionsList,
			}).GetProxy()

			if err != nil {
				panic(err)
			}

			log.Printf("Starting HTTP proxy server on %s", proxyAddr)
			log.Fatal(proxy.ListenAndServe())
		},
	}

	efinProxyCmd.Flags().StringVarP(
		&proxyAddr,
		"local-addr",
		"l",
		DefaultAddr,
		"Local address where the proxy listens for connections",
	)

	efinProxyCmd.Flags().StringVarP(
		&domainRe,
		"scope",
		"s",
		DefaultScope,
		"Regex scope",
	)

	efinProxyCmd.Flags().StringVarP(
		&grpcAddr,
		"grpc-addr",
		"g",
		DefaultGRPCAddr,
		"Start GRPC hooks server on the specified address",
	)

	efinProxyCmd.Flags().StringVarP(
		&excludedExtensions,
		"exclude-extensions",
		"E",
		DefaultExcludeExtensions,
		"Comma separated list of file extensions to exclude",
	)

	efinProxyCmd.Flags().BoolVarP(
		&printLogs,
		"print",
		"p",
		DefaultPrint,
		"Enable raw request/response logging to stdout",
	)

	efinProxyCmd.Flags().StringVarP(
		&dbFile,
		"db-file",
		"D",
		DefaultDBFile,
		"Save requests and responses in the specified Sqlite3 db file",
	)

	efinProxyCmd.Flags().StringVarP(
		&saveDir,
		"save-directory",
		"d",
		DefaultSaveDir,
		"Save each request and response to files in the specified directory",
	)

	efinProxyCmd.Flags().StringVarP(
		&certFile,
		"cert",
		"c",
		DefaultCertFile,
		"Path to Root CA certificate file (PEM)",
	)

	efinProxyCmd.Flags().StringVarP(
		&keyFile,
		"key",
		"k",
		DefaultKeyFile,
		"Path to Root CA private key file (PEM)",
	)

	efinProxyCmd.MarkFlagsRequiredTogether("cert", "key")

	return efinProxyCmd
}
