package persist

import (
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/build"
	"gitlab.com/NebulousLabs/log"
)

const (
	// ErrorLogPrefix defines prefix for error log messages
	// TODO: Remove ErrorLogPrefix, use logger.Errorf when NebulousLabs/log is
	// updated
	ErrorLogPrefix = "ERROR"
)

// Logger is a wrapper for log.Logger.
type Logger struct {
	*log.Logger
}

var (
	// options contains log options with Sia Antfarm- and build-specific
	// information.
	options = log.Options{
		BinaryName:   "Sia Antfarm",
		BugReportURL: build.IssuesURL,
		Debug:        build.DEBUG,
		Release:      buildReleaseType(),
		Version:      build.Version,
	}
)

// NewFileLogger returns a logger that logs to logFilename. The file is opened
// in append mode, and created if it does not exist.
func NewFileLogger(logFilename string) (*Logger, error) {
	logger, err := log.NewFileLogger(logFilename, options)
	return &Logger{logger}, err
}

// buildReleaseType returns the release type for this build, defaulting to
// Release.
func buildReleaseType() log.ReleaseType {
	switch build.Release {
	case "standard":
		return log.Release
	case "dev":
		return log.Dev
	case "testing":
		return log.Testing
	default:
		return log.Release
	}
}
