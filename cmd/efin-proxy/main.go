package main

import (
	"fmt"
	"os"
	"regexp"

	efinproxy "github.com/artilugio0/efin-proxy"
	"github.com/artilugio0/efincore"
	"github.com/spf13/cobra"
)

const (
	DefaultAddr                  string = "127.0.0.1:8669"
	DefaultScope                 string = ".*"
	DefaultIncludeFileTypesRegex string = `.*(text|json|xml|plain).*`
	DefaultExcludeFileTypesRegex string = `.*(image|font|audio|video).*`
	DefaultPrinterSeparator      string = "---------- EFIN-PROXY {{.type}} {{.end}}: {{.id}} ----------"
)

func main() {
	proxyCmd := NewProxyCmd("efin-proxy")
	if err := proxyCmd.Execute(); err != nil {
		panic(err)
	}
}

func NewProxyCmd(use string) *cobra.Command {
	var proxyLocalAddr string
	var proxyScope string
	var proxyGrpcAddr string
	var proxyExcludeExtensions string
	var proxyExcludeTypes string
	var proxyIncludeTypes string
	var proxyDBFile string
	var proxyRawSeparator string
	var proxyNoRequest bool
	var proxyNoResponses bool
	var proxySaveDirectory string
	/*
		var proxySaveWebSockets bool
		var proxyDebugMode bool
	*/

	// proxyCmd represents the proxy command
	var proxyCmd = &cobra.Command{
		Use:   use,
		Short: "Run interceptor proxy",
		Run: func(cmd *cobra.Command, args []string) {
			proxy := efincore.NewProxy(proxyLocalAddr)
			proxy.SetDomainRegex(regexp.MustCompile(proxyScope))

			printer, err := efinproxy.NewPrinter(proxyRawSeparator)
			if err != nil {
				panic(err)
			}

			if !proxyNoRequest {
				proxy.AddRequestOutReadHook(efincore.HookRequestReadFunc(printer.PrintRequest))
			}

			if !proxyNoResponses {
				proxy.AddResponseInReadHook(efincore.HookResponseReadFunc(printer.PrintResponse))
			}

			proxy.AddRequestModHook(efinproxy.RemoveHeaderRequest("accept-encoding"))

			if proxySaveDirectory != "" {
				// create dir if it does not exist
				stat, err := os.Stat(proxySaveDirectory)
				if err != nil {
					if !os.IsNotExist(err) {
						panic(err)
					}

					if err := os.Mkdir(proxySaveDirectory, 0755); err != nil {
						panic(err)
					}
				}

				if !stat.IsDir() {
					panic(fmt.Sprintf("the specified path '%s' is not a directory", proxySaveDirectory))
				}

				saver := efinproxy.NewRawRequestSaver(proxySaveDirectory)

				proxy.AddRequestOutReadHook(efincore.HookRequestReadFunc(saver.SaveRequest))
				proxy.AddResponseInReadHook(efincore.HookResponseReadFunc(saver.SaveResponse))
			}

			if proxyGrpcAddr != "" {
				grpcServer := efincore.NewGRPCServer(proxyGrpcAddr)
				go grpcServer.Run()

				proxy.AddRequestInReadHook(efincore.HookRequestReadFunc(grpcServer.RequestInReadHook))
				proxy.AddRequestModHook(efincore.HookRequestModFunc(grpcServer.RequestModHook))
				proxy.AddRequestOutReadHook(efincore.HookRequestReadFunc(grpcServer.RequestOutReadHook))

				proxy.AddResponseInReadHook(efincore.HookResponseReadFunc(grpcServer.ResponseInReadHook))
				proxy.AddResponseModHook(efincore.HookResponseModFunc(grpcServer.ResponseModHook))
				proxy.AddResponseOutReadHook(efincore.HookResponseReadFunc(grpcServer.ResponseOutReadHook))
			}

			if proxyDBFile != "" {
				db, err := efinproxy.NewSqlite(proxyDBFile)
				if err != nil {
					panic(err)
				}

				proxy.AddRequestOutReadHook(efincore.HookRequestReadFunc(db.SaveRequest))
				proxy.AddResponseInReadHook(efincore.HookResponseReadFunc(db.SaveResponse))

				/*
					if proxySaveWebSockets {
						builder.WithSaveWebSockets()
					}
				*/
			}

			if proxyExcludeExtensions != "" {
				filterExtension, err := efincore.ExcludeFileExtensions(proxyExcludeExtensions)
				if err != nil {
					panic(err)
				}
				proxy.AddRequestFilter(filterExtension)
			}

			if proxyExcludeTypes != "" {
				filterFileType, err := efincore.ExcludeFileTypes(
					proxyIncludeTypes,
					proxyExcludeTypes,
				)
				proxy.AddRequestFilter(filterFileType)
				if err != nil {
					panic(err)
				}
			}

			if err := proxy.ListenAndServe(); err != nil {
				panic(err)
			}
		},
	}

	proxyCmd.Flags().StringVarP(
		&proxyLocalAddr,
		"local-addr",
		"l",
		DefaultAddr,
		"Local address where the proxy listens for connections",
	)

	proxyCmd.Flags().StringVarP(
		&proxyScope,
		"scope",
		"s",
		DefaultScope,
		"Regex scope",
	)

	proxyCmd.Flags().StringVarP(
		&proxyGrpcAddr,
		"grpc-addr",
		"g",
		"127.0.0.1:8670",
		"Start GRPC hooks server on the specified address",
	)

	proxyCmd.Flags().StringVarP(
		&proxyExcludeExtensions,
		"exclude-extensions",
		"E",
		"png|gif|jpeg|jpg|aac|ts",
		"Exclude file extensions (regex)",
	)

	proxyCmd.Flags().StringVarP(
		&proxyIncludeTypes,
		"include-types",
		"t",
		DefaultIncludeFileTypesRegex,
		"Allways include file types based on the 'Accept' header (regex), has precedence over exclude types",
	)

	proxyCmd.Flags().StringVarP(
		&proxyExcludeTypes,
		"exclude-types",
		"T",
		DefaultExcludeFileTypesRegex,
		"Exclude file types based on the 'Accept' header (regex)",
	)

	proxyCmd.Flags().StringVarP(
		&proxyDBFile,
		"db-file",
		"D",
		"",
		"Save requests and responses in the specified Sqlite3 db file",
	)

	proxyCmd.Flags().StringVarP(
		&proxyRawSeparator,
		"raw-separator",
		"r",
		DefaultPrinterSeparator,
		"Separator to include between requests for raw output format",
	)

	proxyCmd.Flags().BoolVarP(
		&proxyNoRequest,
		"no-request",
		"Q",
		false,
		"Do not print request",
	)

	proxyCmd.Flags().BoolVarP(
		&proxyNoResponses,
		"no-responses",
		"R",
		false,
		"Do not print responses",
	)

	proxyCmd.Flags().StringVarP(
		&proxySaveDirectory,
		"save-directory",
		"d",
		"",
		"In addition to printing to stdout save each request and response to files in the specified directory",
	)

	/*
		proxyCmd.Flags().BoolVarP(
			&proxySaveWebSockets,
			"save-websockets",
			"w",
			false,
			"Save websocket frames in the db (-D flag must be set)",
		)

		proxyCmd.Flags().BoolVarP(
			&proxyDebugMode,
			"debug-mode",
			"",
			false,
			"Enables debug mode",
		)
	*/

	return proxyCmd
}
