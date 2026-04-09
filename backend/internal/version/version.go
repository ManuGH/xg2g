package version

var (
	// Version is the current application version.
	// It should be populated by the build system (ldflags) or fall back to the VERSION file.
	Version = "v3.4.6" // Fallback matching current RELEASE

	// Commit is the git short hash of the build.
	Commit = "unknown"

	// Date is the build timestamp.
	Date = "unknown"
)
