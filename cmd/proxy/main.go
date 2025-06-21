package main

import (
	"github.com/artilugio0/proxy-vibes/pkg/cmd"
	_ "modernc.org/sqlite" // SQLite driver
)

func main() {
	cmd.Execute()
}
