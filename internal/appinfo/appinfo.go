// Package appinfo holds build information injected at link time with
// -ldflags "-X github.com/borud/amplifi/internal/appinfo.Version=...".
package appinfo

var (
	// Version is the release tag the binary was built from, or "dev".
	Version = "dev"
	// Commit is the git commit hash the binary was built from.
	Commit = "unknown"
	// BuildTime is when the binary was built, in RFC 3339 UTC.
	BuildTime = "unknown"
)
