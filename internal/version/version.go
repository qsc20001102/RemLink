// Package version exposes build metadata shared by all RemLink binaries.
package version

var (
	// Version is overridden by release builds with -ldflags.
	Version = "dev"
	// Commit is overridden by release builds with -ldflags.
	Commit = "unknown"
)

// String returns a compact version suitable for logs and command output.
func String() string {
	if Commit == "" || Commit == "unknown" {
		return Version
	}
	return Version + "+" + Commit
}
