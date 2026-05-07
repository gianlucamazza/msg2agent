// Package buildinfo exposes build metadata injected at link time via -ldflags.
package buildinfo

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// String returns a human-readable build summary.
func String() string {
	return Version + " (" + Commit + ", " + Date + ")"
}
