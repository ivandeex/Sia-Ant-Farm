package ant

import (
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
	dataDir := test.TestDir(t.Name())
	config := newTestingSiadConfig(dataDir)

	// Create logger
	logger := test.NewTestLogger(t, dataDir)
	defer func() {
		if err := logger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Create siad process
	siad, err := newSiad(logger, config)
	if err != nil {
		t.Fatal(err)
	}
	defer stopSiad(logger, config.DataDir, config.APIAddr, config.APIPassword, siad.Process)

	// Create ant client
	c, err := newClient(config.APIAddr, config.APIPassword)
	if err != nil {
		t.Fatal(err)
	}

	// Create ant
	ant := &Ant{
		staticAntsSyncWG: &sync.WaitGroup{},
		staticLogger:     logger,
		StaticClient:     c,
	}

	// Create jobRunnner on same APIAddr as the siad process
	j, err := newJobRunner(logger, ant, config.DataDir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := j.Stop(); err != nil {
			t.Fatal(err)
		}
	}()

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
			ant.staticLogger.Errorf("%v: error getting wallet info: %v", dataDir, err)
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

	// Create hostJobRunner
	hjr, err := j.newHostJobRunner()
	if err != nil {
		t.Fatal()
	}

	// Check no host announcement transaction in blockchain
	cg, err := j.staticClient.ConsensusGet()
	if err != nil {
		t.Fatal(err)
	}
	blockHeightBeforeAnnouncement := cg.Height
	found, err := hjr.managedAnnouncementTransactionInBlockRange(types.BlockHeight(0), blockHeightBeforeAnnouncement)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("host announcement transaction should not yet be in the blockchain")
	}

	// Announce host
	err = hjr.staticClient.HostAnnouncePost()
	if err != nil {
		t.Fatal(err)
	}

	// Wait for host announcement transaction in blockchain.
	// Test waitAnnounceTransactionInBlockchain().
	err = hjr.managedWaitAnnounceTransactionInBlockchain()
	if err != nil {
		t.Fatal(err)
	}

	// Check host announcement transaction in block range.
	// Test announcementTransactionInBlockRange().
	cg, err = j.staticClient.ConsensusGet()
	if err != nil {
		t.Fatal(err)
	}
	found, err = hjr.managedAnnouncementTransactionInBlockRange(blockHeightBeforeAnnouncement+1, cg.Height)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("host announcement was not found in the block range")
	}

	// Check host announcement transaction in the specific block.
	// Test announcementTransactionInBlock().
	found, err = hjr.announcementTransactionInBlock(hjr.managedAnnouncedBlockHeight())
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("host announcement was not found in the specific block")
	}
}
