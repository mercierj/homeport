package version

// Version information set via ldflags at build time
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// Info returns formatted version information
func Info() string {
	return "Version: " + Version + "\nCommit: " + Commit + "\nBuild Date: " + Date
}
