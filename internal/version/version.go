/*
Build‑time versioning (internal/version + ldflags)
Overridden at build time to show real metadata.
*/

package version

// These variables can be set at build time with -ldflags:
// -X 'github.com/ascandal/limitwatch/internal/version.Version=...'
// -X 'github.com/ascandal/limitwatch/internal/version.Commit=...'
// -X 'github.com/ascandal/limitwatch/internal/version.Date=...'
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)
