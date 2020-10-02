package ant

import (
	"path/filepath"
	"sync"
	"testing"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/persist"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/test"
)

// NewAntsCommon is a test helper function to prepare antsCommon with a logger
// for a test. Logger should be closed by a caller.
func NewAntsCommon(t *testing.T, dataDir string) AntsCommon {
	// Prepare logger
	logPath := filepath.Join(dataDir, test.AntfarmTestLog)
	logger, err := persist.NewFileLogger(logPath)
	if err != nil {
		t.Fatal(err)
	}

	// Prepare antsCommon
	antsCommon := AntsCommon{
		AntsSyncWG:   &sync.WaitGroup{},
		Logger:       logger,
		CallerLogStr: test.AntfarmTest + " " + t.Name(),
	}

	return antsCommon
}
