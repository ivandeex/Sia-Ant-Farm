package ant

import (
	"io/ioutil"
	"os"
	"testing"
)

func TestNewJobRunner(t *testing.T) {
	t.Parallel()
	datadir, err := ioutil.TempDir("", "testing-data")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(datadir)
	config := SiadConfig{
		APIAddr:      "localhost:31337",
		APIPassword:  "",
		DataDir:      datadir,
		HostAddr:     "localhost:31339",
		RPCAddr:      "localhost:31338",
		SiadPath:     "siadDir",
		SiaMuxAddr:   "localhost:31340",
		SiaMuxWsAddr: "localhost:31341",
	}
	siad, err := newSiad(config)
	if err != nil {
		t.Fatal(err)
	}
	defer stopSiad("localhost:31337", siad.Process)

	j, err := newJobRunner("localhost:31337", "", datadir)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Stop()
}
