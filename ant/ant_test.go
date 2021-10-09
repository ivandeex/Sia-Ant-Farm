package ant

import (
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"go.sia.tech/sia-antfarm/test"
	"go.sia.tech/siad/node/api/client"
	"go.sia.tech/siad/types"
	"gitlab.com/NebulousLabs/errors"
)

// newTestingAntConfig creates an AntConfig for testing.
func newTestingAntConfig(datadir string) (AntConfig, error) {
	sc, err := newTestingSiadConfig(datadir)
	if err != nil {
		return AntConfig{}, errors.AddContext(err, "can't create new siad config")
	}
	return AntConfig{SiadConfig: sc}, nil
}

// TestClosingAnt tests closing the created Ant
func TestClosingAnt(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create testing config
	dataDir := test.TestDir(t.Name())
	config, err := newTestingAntConfig(dataDir)
	if err != nil {
		t.Fatal(err)
	}

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

// TestInitWalletExistingSeed tests starting ant and initializing its wallet
// with an existing seed
func TestInitWalletExistingSeed(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create testing config with the existing seed
	dataDir := test.TestDir(t.Name())
	config, err := newTestingAntConfig(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	config.InitialWalletSeed = test.WalletSeed1

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
	opts.Password = config.APIPassword
	c := client.New(opts)

	// Check wallet seed
	wsg, err := c.WalletSeedsGet()
	if err != nil {
		t.Fatal(err)
	}
	if wsg.PrimarySeed != test.WalletSeed1 {
		t.Fatalf("unexpected wallet seed, want: %v, got: %v", test.WalletSeed1, wsg.PrimarySeed)
	}

	// Check wallet address
	wag, err := c.WalletAddressesGet()
	if err != nil {
		t.Fatal(err)
	}
	if wag.Addresses[0].String() != test.WalletSeed1Address1 {
		t.Fatalf("unexpected wallet address, want: %v, got: %v", test.WalletSeed1Address1, wag.Addresses[0])
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
	config, err := newTestingAntConfig(dataDir)
	if err != nil {
		t.Fatal(err)
	}

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
	config, err := newTestingAntConfig(dataDir)
	if err != nil {
		t.Fatal(err)
	}

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
	config, err := newTestingAntConfig(dataDir)
	if err != nil {
		t.Fatal(err)
	}

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
	err = ant.UpdateSiad(newSiadPath)
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
	config, err := newTestingAntConfig(dataDir)
	if err != nil {
		t.Fatal(err)
	}

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
