// Package config embeds the committed hosted configuration.
package config

import _ "embed"

// Hosted contains administrator defaults for hosted deployments.
//
//go:embed hosted.json
var Hosted []byte
