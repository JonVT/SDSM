package version

// Package version holds build-time metadata injected via -ldflags.
// All fields are expected to be set at build time. When not set, helpers
// provide sensible development defaults.

var (
	// Version is a SemVer tag like v1.2.3 for releases. Empty for dev builds.
	Version = ""
	// Commit is the short git SHA for the build.
	Commit = ""
	// Date is the UTC build timestamp in RFC3339 format.
	Date = ""
	// Dirty is "dirty" when the working tree had uncommitted changes, otherwise "clean".
	Dirty = ""
)

// String returns a compact human-readable version for display in the UI.
// For releases, returns Version. For dev builds, returns e.g. "dev-<sha>*" when dirty
// or "dev-<sha>" when clean. If no metadata is available, returns "dev".
func String() string {
	if Version != "" {
		return Version
	}
	if Commit != "" {
		suffix := Commit
		if Dirty == "dirty" {
			suffix += "*"
		}
		return "dev-" + suffix
	}
	return "dev"
}
