package ant

import (
	"testing"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/test"
	"gitlab.com/NebulousLabs/Sia/node/api/client"
	"gitlab.com/NebulousLabs/Sia/persist"
)

// newTestingSiadConfig creates a generic SiadConfig for the provided datadir.
func newTestingSiadConfig(datadir string) SiadConfig {
	return SiadConfig{
		AllowHostLocalNetAddress: true,
		APIAddr:                  test.RandomLocalAddress(),
		APIPassword:              persist.RandomSuffix(),
		DataDir:                  datadir,
		HostAddr:                 test.RandomLocalAddress(),
		RPCAddr:                  test.RandomLocalAddress(),
		SiadPath:                 test.TestSiadFilename,
		SiaMuxAddr:               test.RandomLocalAddress(),
		SiaMuxWsAddr:             test.RandomLocalAddress(),
	}
}

// TestNewSiad tests that NewSiad creates a reachable Sia API
func TestNewSiad(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create testing config
	datadir := test.TestDir(t.Name())
	config := newTestingSiadConfig(datadir)

	// Create the siad process
	siad, err := newSiad(config)
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

	// Test Client by pinging the ConsensusGet endpoint
	if _, err := c.ConsensusGet(); err != nil {
		t.Error(err)
	}

	// Stop siad process
	stopSiad(config.APIAddr, config.APIPassword, siad.Process)

	// Test Creating siad with a blank config
	_, err = newSiad(SiadConfig{})
	if err == nil {
		t.Fatal("Shouldn't be able to create siad process with empty config")
	}

	// verify that NewSiad returns an error given invalid args
	config.APIAddr = "this_is_an_invalid_address:1000000"
	_, err = newSiad(config)
	if err == nil {
		t.Fatal("expected newsiad to return an error with invalid args")
	}
}
