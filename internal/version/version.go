// Package version is the single source-of-truth accessor for the
// project version (major.minor.build).
//
// The canonical value lives in internal/version/VERSION
// (symlinked as <repo-root>/VERSION for discoverability).
// It is embedded at build time so the binary always reports the same
// version that packaging uses. To bump the version, just edit VERSION,
// e.g. to increase the build number. No other file needs to change.
//
// A release/CI build may still override it without editing files:
//   go build -ldflags "-X github.com/galets/gatefile/internal/version.override=1.2.3" ./...
package version

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var versionTxt string

// override is set via -ldflags "-X ...version.override=X.Y.Z".
// Empty means "use VERSION file".
var override string

// Version returns the current major.minor.build version.
func Version() string {
	if s := strings.TrimSpace(override); s != "" {
		return s
	}
	return strings.TrimSpace(versionTxt)
}
