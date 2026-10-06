package buildinfo

import (
	"embed"
	"strings"
)

//go:embed VERSION
var versionFS embed.FS

// Commit may be injected by ldflags without duplicating the version source.
var Commit = "unknown"

// Version returns the embedded VERSION value.
func Version() string { v, _ := versionFS.ReadFile("VERSION"); return strings.TrimSpace(string(v)) }
