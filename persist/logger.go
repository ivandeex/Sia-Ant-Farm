package persist

import (
	"os"
	"path/filepath"

	"go.sia.tech/sia-antfarm/build"
	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/NebulousLabs/log"
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
func NewFileLogger(logFilepath string) (*Logger, error) {
	// Create a dir if it doesn't exist
	logDir := filepath.Dir(logFilepath)
	err := os.MkdirAll(logDir, 0700)
	if err != nil {
		return nil, errors.AddContext(err, "can't create logger dir(s)")
	}

	logger, err := log.NewFileLogger(logFilepath, options)
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
