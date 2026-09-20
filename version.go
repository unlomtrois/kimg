package main

import (
	"runtime/debug"
	"strings"
)

// version can be stamped at link time for builds made outside the module
// system, such as the container build:
//
//	go build -ldflags="-X main.version=v0.1.0"
//
// When it is empty, the version comes from the metadata the go tool records
// in the binary, so `go install ...@v0.1.0` reports v0.1.0 with no flags.
var version = ""

func buildVersion() string {
	if version != "" {
		return version
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}

	// A local build has no module version, but the go tool still stamps the
	// commit it was built from.
	var revision, suffix string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				suffix = "-dirty"
			}
		}
	}
	if revision == "" {
		return "devel"
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	return strings.TrimSpace(revision + suffix)
}
