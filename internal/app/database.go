package app

import "github.com/jtoosh/baseball-mcp/internal/data"

type Database = data.Database
type QueryResult = data.Result

var OpenDatabase = data.Open
var ImportCSV = data.Import
