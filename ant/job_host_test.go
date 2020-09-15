package ant

import (
	"log"
	"sync"
	"testing"
	"time"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/test"
	"gitlab.com/NebulousLabs/Sia/node/api/client"
	"gitlab.com/NebulousLabs/Sia/types"
)

// TestAnnounceHost tests creating a new host job runner and announces host
func TestAnnounceHost(t *testing.T) {
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

	// Mine at least 50,000 SC
	desiredbalance := types.NewCurrency64(50000).Mul(types.SiacoinPrecision)
	go j.balanceMaintainer(desiredbalance)
	start := time.Now()
	for {
		select {
		case <-j.StaticTG.StopChan():
			return
		case <-time.After(miningCheckFrequency):
		}
		walletInfo, err := j.staticClient.WalletGet()
		if err != nil {
			log.Printf("[ERROR] [host] [%v] Error getting wallet info: %v\n", j.staticSiaDirectory, err)
			continue
		}
		if walletInfo.ConfirmedSiacoinBalance.Cmp(desiredbalance) > 0 {
			break
		}
		if time.Since(start) > miningTimeout {
			t.Fatalf("couldn't mine enough currency within %v timeout", miningTimeout)
		}
	}

	// Set netAddress
	netAddress := config.HostAddr
	err = j.staticClient.HostModifySettingPost(client.HostParamNetAddress, netAddress)
	if err != nil {
		t.Fatal(err)
	}

	// Create hostJobRunner, check no announcement transaction in blockchain
	hjr := j.newHostJobRunner()
	found, err := hjr.announcementTransactionInBlock(1)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("host announcement transaction should not yet be in the blockchain")
	}

	// Announce host
	err = hjr.announce()
	if err != nil {
		t.Fatal(err)
	}
}
