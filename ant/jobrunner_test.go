package ant

import (
	"sync"
	"testing"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/test"
)

// TestNewJobRunner test creating a new Job Runner
func TestNewJobRunner(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create testing config
	dataDir := test.TestDir(t.Name())
	config := newTestingSiadConfig(dataDir)

	// Create siad process
	siad, err := newSiad(config)
	if err != nil {
		t.Fatal(err)
	}
	defer stopSiad(config.APIAddr, config.APIPassword, siad.Process)

	// Create logger
	logger := test.NewTestLogger(t, dataDir)
	defer func() {
		if err := logger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Create ant
	ant := &Ant{
		staticAntsSyncWG: &sync.WaitGroup{},
		staticLogger:     logger,
	}

	// Create jobRunnner on same APIAddr as the siad process
	j, err := newJobRunner(logger, ant, config.APIAddr, config.APIPassword, config.DataDir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer j.Stop()
}
