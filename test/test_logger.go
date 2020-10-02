package test

import (
	"log"
	"path/filepath"
	"testing"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/persist"
)

// NewTestLogger creates a new temporary test directory and a logger for a
// test. This should only every be called once per test. Otherwise it will
// delete the directory again.
func NewTestLogger(t *testing.T) *persist.Logger {
	testDir := TestDir(t.Name())
	testLogPath := filepath.Join(testDir, AntfarmTestLog)
	testLogger, err := persist.NewFileLogger(testLogPath)
	if err != nil {
		t.Fatal(err)
	}
	log.Printf("INFO antfarm-test %v: This test logs to: %v", t.Name(), testLogPath)
	return testLogger
}
