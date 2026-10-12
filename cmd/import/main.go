// Command import builds a snapshot database outside the running application.
package main

import (
	"flag"
	"github.com/jtoosh/baseball-mcp/internal/data"
	"log"
)

func main() {
	source := flag.String("csv", "advanced-mlb-24-26.csv", "MLB export")
	database := flag.String("db", "mlb.db", "output snapshot database")
	flag.Parse()
	if err := data.Import(*source, *database); err != nil {
		log.Fatal(err)
	}
	log.Print("Imported snapshot successfully")
}
