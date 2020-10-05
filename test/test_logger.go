package test

import (
	"log"
	"path/filepath"
	"testing"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/persist"
)

// NewTestLogger creates a logger for a test.
func NewTestLogger(t *testing.T, dataDir string) *persist.Logger {
	testLogPath := filepath.Join(dataDir, "antfarm-test.log")
	testLogger, err := persist.NewFileLogger(testLogPath)
	if err != nil {
		t.Fatal(err)
	}
	log.Printf("INFO antfarm-test %v: This test logs to: %v", t.Name(), testLogPath)
	return testLogger
}
