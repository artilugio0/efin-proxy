package main

import (
	"github.com/artilugio0/efin-proxy/pkg/cmd"
	_ "modernc.org/sqlite" // SQLite driver
)

func main() {
	cmd.Execute()
}
