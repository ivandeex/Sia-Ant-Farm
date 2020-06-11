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

	siad, err := newSiad("siad", datadir, "localhost:9990", "localhost:0", "localhost:0", "")
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
	_, err = newSiad("siad", datadir, "this_is_an_invalid_addres:1000000", "localhost:0", "localhost:0", "")
	if err == nil {
		t.Fatal("expected newsiad to return an error with invalid args")
	}
}
