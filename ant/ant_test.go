package ant

import (
	"sync"
	"testing"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/test"
	"gitlab.com/NebulousLabs/Sia/node/api/client"
	"gitlab.com/NebulousLabs/Sia/types"
)

// newTestingAntConfig creates an AntConfig for testing.
func newTestingAntConfig(datadir string) AntConfig {
	return AntConfig{SiadConfig: newTestingSiadConfig(datadir)}
}

// TeTestNewAnt tests creating an Ant
func TestNewAnt(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create testing config
	datadir := test.TestDir(t.Name())
	config := newTestingAntConfig(datadir)

	// Create Ant
	ant, err := New(&sync.WaitGroup{}, config)
	if err != nil {
		t.Fatal(err)
	}
	defer ant.Close()

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

// TTestStartJob probes the StartJob method of the ant
func TestStartJob(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create testing config
	datadir := test.TestDir(t.Name())
	config := newTestingAntConfig(datadir)

	// Create Ant
	ant, err := New(&sync.WaitGroup{}, config)
	if err != nil {
		t.Fatal(err)
	}
	defer ant.Close()

	// Nonexistent job should throw an error
	err = ant.StartJob(&sync.WaitGroup{}, "thisjobdoesnotexist")
	if err == nil {
		t.Fatal("StartJob should return an error with a nonexistent job")
	}
}

// TestWTestWalletAddress tests getting a wallet address for an initialize ant
func TestWalletAddress(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create testing config
	datadir := test.TestDir(t.Name())
	config := newTestingAntConfig(datadir)

	// Create Ant
	ant, err := New(&sync.WaitGroup{}, config)
	if err != nil {
		t.Fatal(err)
	}
	defer ant.Close()

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
