package ant

import (
	"sync"
	"testing"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/test"
)

// NewAntsCommon is a test helper function to prepare antsCommon with a logger
// for a test.
func NewAntsCommon(t *testing.T, dataDir string) AntsCommon {
	// Prepare logger
	logger := test.NewTestLogger(t, dataDir)

	// Prepare antsCommon
	antsCommon := AntsCommon{
		AntsSyncWG:    &sync.WaitGroup{},
		Logger:        logger,
		CallerDataDir: dataDir,
	}

	return antsCommon
}
