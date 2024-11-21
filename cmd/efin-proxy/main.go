package main

import (
	efinproxy "github.com/artilugio0/efin-proxy"
)

func main() {
	proxyCmd := efinproxy.NewProxyCmd("efin-proxy")
	if err := proxyCmd.Execute(); err != nil {
		panic(err)
	}
}
