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

// TestAnnounceHost tests host announcement, host job runner and its methods.
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

	// Mine at least 50,000 SC for host announcement.
	// Keep mining so that host announcement gets to blockchain.
	initialbalance := types.NewCurrency64(50e3).Mul(types.SiacoinPrecision)
	desidedBalance := types.NewCurrency64(5e9).Mul(types.SiacoinPrecision)
	go j.balanceMaintainer(desidedBalance)
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
		if walletInfo.ConfirmedSiacoinBalance.Cmp(initialbalance) > 0 {
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

	// Create hostJobRunner, check no host announcement transaction in blockchain
	hjr := j.newHostJobRunner()
	cg, err := j.staticClient.ConsensusGet()
	if err != nil {
		t.Fatal(err)
	}
	blockHeightBeforeAnnouncement := cg.Height
	found, err := hjr.announcementTransactionInBlockRange(types.BlockHeight(0), blockHeightBeforeAnnouncement)
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

	// Wait for host announcement transaction in blockchain
	err = hjr.waitAnnounceTransactionInBlockchain()
	if err != nil {
		t.Fatal(err)
	}

	// Check host announcement transaction in block range
	cg, err = j.staticClient.ConsensusGet()
	if err != nil {
		t.Fatal(err)
	}
	found, err = hjr.announcementTransactionInBlockRange(blockHeightBeforeAnnouncement+1, cg.Height)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("host announcement was not found in the block range")
	}

	// Check host announcement transaction in a specific block
	found, err = hjr.announcementTransactionInBlock(hjr.announcedBlockHeight)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("host announcement was not found in the specific block")
	}
}
