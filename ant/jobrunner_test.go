package ant

import (
	"sync"
	"testing"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/test"
	"gitlab.com/NebulousLabs/Sia/node/api/client"
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

	// Create logger
	logger := test.NewTestLogger(t, dataDir)
	defer func() {
		if err := logger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Create siad process
	siad, err := newSiad(logger, config)
	if err != nil {
		t.Fatal(err)
	}
	defer stopSiad(logger, config.DataDir, config.APIAddr, config.APIPassword, siad.Process)

	// Create ant client
	opts, err := client.DefaultOptions()
	if err != nil {
		t.Fatal(err)
	}
	opts.Address = config.APIAddr
	opts.Password = config.APIPassword
	client := &client.Client{Options: opts}

	// Create ant
	ant := &Ant{
		staticAntsSyncWG: &sync.WaitGroup{},
		staticLogger:     logger,
		StaticClient:     client,
	}

	// Create jobRunnner on same APIAddr as the siad process
	j, err := newJobRunner(logger, ant, config.DataDir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := j.Stop(); err != nil {
			t.Fatal(err)
		}
	}()
}
