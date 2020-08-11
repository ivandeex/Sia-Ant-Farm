package ant

import (
	"sync"
	"testing"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/test"
)

// TeTestNewJobRunner test creating a new Job Runner
func TestNewJobRunner(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create testing config
	datadir := test.TestDir(t.Name())
	config := newTestingSiadConfig(datadir)

	// Create siad process
	siad, err := newSiad(config)
	if err != nil {
		t.Fatal(err)
	}
	defer stopSiad(config.APIAddr, siad.Process)

	// Create jobRunnner on same APIAddr as the siad process
	j, err := newJobRunner(&sync.WaitGroup{}, &Ant{}, config.APIAddr, config.APIPassword, config.DataDir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer j.Stop()
}
