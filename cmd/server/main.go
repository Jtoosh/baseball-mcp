// Command server runs the baseball question application.
package main

import (
	"flag"

	"github.com/jtoosh/baseball-mcp/internal/app"
)

func main() {
	path := flag.String("config", "config.json", "administrator JSON configuration")
	flag.Parse()
	app.Run(*path)
}
