package test

import (
	"path/filepath"
	"testing"

	"go.sia.tech/sia-antfarm/persist"
)

// NewTestLogger creates a logger for a test.
func NewTestLogger(t *testing.T, dataDir string) *persist.Logger {
	testLogPath := filepath.Join(dataDir, "antfarm-test.log")
	testLogger, err := persist.NewFileLogger(testLogPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Test logs are stored at: %v", testLogPath)
	return testLogger
}
