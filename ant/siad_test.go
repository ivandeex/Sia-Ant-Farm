package ant

import (
	"io/ioutil"
	"os"
	"testing"

	"gitlab.com/NebulousLabs/Sia/node/api/client"
)

// TestNewSiad tests that NewSiad creates a reachable Sia API
func TestNewSiad(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	datadir, err := ioutil.TempDir("", "sia-testing")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(datadir)

	config := SiadConfig{
		APIAddr:      "localhost:9990",
		APIPassword:  "",
		DataDir:      datadir,
		HostAddr:     "localhost:0",
		RPCAddr:      "localhost:0",
		SiadPath:     "siad",
		SiaMuxAddr:   "localhost:0",
		SiaMuxWsAddr: "localhost:0",
	}

	siad, err := newSiad(config)
	if err != nil {
		t.Fatal(err)
	}
	defer siad.Process.Kill()

	opts, err := client.DefaultOptions()
	if err != nil {
		t.Fatal(err)
	}
	opts.Address = "localhost:9990"
	c := client.New(opts)
	if _, err := c.ConsensusGet(); err != nil {
		t.Error(err)
	}
	siad.Process.Kill()

	// verify that NewSiad returns an error given invalid args
	config.APIAddr = "this_is_an_invalid_addres:1000000"
	_, err = newSiad(config)
	if err == nil {
		t.Fatal("expected newsiad to return an error with invalid args")
	}
}
