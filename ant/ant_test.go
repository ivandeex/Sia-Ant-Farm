package ant

import (
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/test"
	"gitlab.com/NebulousLabs/Sia/node/api/client"
	"gitlab.com/NebulousLabs/Sia/types"
)

// newTestingAntConfig creates an AntConfig for testing.
func newTestingAntConfig(datadir string) AntConfig {
	return AntConfig{SiadConfig: newTestingSiadConfig(datadir)}
}

// TestClosingAnt tests closing the created Ant
func TestClosingAnt(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create testing config
	dataDir := test.TestDir(t.Name())
	config := newTestingAntConfig(dataDir)

	// Create logger
	logger := test.NewTestLogger(t, dataDir)
	defer func() {
		if err := logger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Create Ant
	ant, err := New(&sync.WaitGroup{}, logger, config)
	if err != nil {
		t.Fatal(err)
	}

	// Create Sia Client
	opts, err := client.DefaultOptions()
	if err != nil {
		t.Fatal(err)
	}
	opts.Address = config.APIAddr
	c := client.New(opts)

	// Test Sia is running by calling ConsensusGet endpoint
	if _, err = c.ConsensusGet(); err != nil {
		t.Fatal(err)
	}

	// Close the ant
	err = ant.Close()
	if err != nil {
		t.Fatal(err)
	}

	// Wait enough time for ant.Close(), stopSiad() to try to finish
	timeout := stopSiadTimeout + 10*time.Second
	siadPath := config.SiadPath
	APIAddress := config.APIAddr

	start := time.Now()
checkingLoop:
	for {
		// Timeout reached
		if time.Since(start) > timeout {
			t.Fatal("Siad process is still running")
		}

		// Get processes
		out, err := exec.Command("ps", "-eo", "cmd").Output()
		if err != nil {
			t.Fatal("can't execute ps command")
		}

		// Parse processes
		for _, p := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(p, siadPath) && strings.Contains(p, " --api-addr="+APIAddress+" ") {
				// Siad process is still running
				time.Sleep(time.Second)
				continue checkingLoop
			}
		}

		// Siad process is not running
		return
	}
}

// TestNewAnt tests creating an Ant
func TestNewAnt(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create testing config
	dataDir := test.TestDir(t.Name())
	config := newTestingAntConfig(dataDir)

	// Create logger
	logger := test.NewTestLogger(t, dataDir)
	defer func() {
		if err := logger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Create Ant
	ant, err := New(&sync.WaitGroup{}, logger, config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := ant.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Create Sia Client
	opts, err := client.DefaultOptions()
	if err != nil {
		t.Fatal(err)
	}
	opts.Address = config.APIAddr
	c := client.New(opts)

	// Test Sia Client works by calling ConsensusGet endpoint
	if _, err = c.ConsensusGet(); err != nil {
		t.Fatal(err)
	}
}

// TestStartJob probes the StartJob method of the ant
func TestStartJob(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create testing config
	dataDir := test.TestDir(t.Name())
	config := newTestingAntConfig(dataDir)

	// Create logger
	logger := test.NewTestLogger(t, dataDir)
	defer func() {
		if err := logger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Create Ant
	ant, err := New(&sync.WaitGroup{}, logger, config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := ant.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Nonexistent job should throw an error
	err = ant.StartJob(&sync.WaitGroup{}, "thisjobdoesnotexist")
	if err == nil {
		t.Fatal("StartJob should return an error with a nonexistent job")
	}
}

// TestUpdateAnt verifies that ant can be updated using new siad binary path.
// The test doesn't verify that ant's jobs will continue to run correctly.
func TestUpdateAnt(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create testing config
	dataDir := test.TestDir(t.Name())
	config := newTestingAntConfig(dataDir)

	// Create logger
	logger := test.NewTestLogger(t, dataDir)
	defer func() {
		if err := logger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Create Ant
	ant, err := New(&sync.WaitGroup{}, logger, config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := ant.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Create Sia Client
	opts, err := client.DefaultOptions()
	if err != nil {
		t.Fatal(err)
	}
	opts.Address = config.APIAddr
	c := client.New(opts)

	// Test the ant works by calling ConsensusGet endpoint
	if _, err = c.ConsensusGet(); err != nil {
		t.Fatal(err)
	}

	// Update ant
	newSiadPath := test.RelativeSiadPath()
	err = ant.UpdateSiad(logger, newSiadPath)
	if err != nil {
		t.Fatal(err)
	}

	// Test the updated ant works by calling ConsensusGet endpoint
	if _, err = c.ConsensusGet(); err != nil {
		t.Fatal(err)
	}
}

// TestWalletAddress tests getting a wallet address for an initialize ant
func TestWalletAddress(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create testing config
	dataDir := test.TestDir(t.Name())
	config := newTestingAntConfig(dataDir)

	// Create logger
	logger := test.NewTestLogger(t, dataDir)
	defer func() {
		if err := logger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Create Ant
	ant, err := New(&sync.WaitGroup{}, logger, config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := ant.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Get wallet address
	addr, err := ant.WalletAddress()
	if err != nil {
		t.Fatal(err)
	}
	blankaddr := types.UnlockHash{}
	if *addr == blankaddr {
		t.Fatal("WalletAddress returned an empty address")
	}
}
