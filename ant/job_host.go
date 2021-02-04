package ant

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gitlab.com/NebulousLabs/Sia/modules"
	"gitlab.com/NebulousLabs/Sia/node/api/client"
	"gitlab.com/NebulousLabs/Sia/types"
	"gitlab.com/NebulousLabs/errors"
)

const (
	// hostAnnounceBlockHeightDelay defines how many blocks we wait for host
	// announce transaction to apeear in confirmed transactions. If the
	// transaction doesn't appear within the interval, we announce host again.
	hostAnnounceBlockHeightDelay = types.BlockHeight(20)

	// hostAPIErrorFrequency defines frequency at which we retry unsuccessful
	// API call.
	hostAPIErrorFrequency = time.Second * 5

	// hostTransactionCheckFrequency defines frequency at which we check
	// announce host transaction.
	hostTransactionCheckFrequency = time.Second * 4

	// hostLoopFrequency defines frequency at which we execute main host job
	// loop.
	hostLoopFrequency = time.Second * 10

	// miningTimeout defines timeout for mining desired balance
	miningTimeout = time.Minute * 5

	// miningCheckFrequency defines how often the host will check for desired
	// balance during mining
	miningCheckFrequency = time.Second
)

var (
	// errAntStopped defines a reusable error when ant was stopped
	errAntStopped = errors.New("ant was stopped")
)

// hostJobRunner extends generic jobRunner with host specific fields.
type hostJobRunner struct {
	*JobRunner
	announced            bool
	announcedBlockHeight types.BlockHeight
	lastStorageRevenue   types.Currency
	mu                   sync.Mutex
	staticHostNetAddress modules.NetAddress
}

// jobHost unlocks the wallet, mines some currency, and starts a host offering
// storage to the ant farm.
func (j *JobRunner) jobHost() {
	err := j.StaticTG.Add()
	if err != nil {
		j.staticLogger.Errorf("%v: can't add thread group: %v", j.staticDataDir, err)
		return
	}
	defer j.StaticTG.Done()

	// Wait for ants to be synced if the wait group was set
	synced := j.waitForAntsSync()
	if !synced {
		j.staticLogger.Errorf("%v: waiting for ants to sync failed", j.staticDataDir)
		return
	}

	// Mine at least 50,000 SC
	desiredbalance := types.NewCurrency64(50000).Mul(types.SiacoinPrecision)
	start := time.Now()
	for {
		select {
		case <-j.StaticTG.StopChan():
			return
		case <-time.After(miningCheckFrequency):
		}
		walletInfo, err := j.staticClient.WalletGet()
		if err != nil {
			j.staticLogger.Errorf("%v: error getting wallet info: %v", j.staticDataDir, err)
			continue
		}
		if walletInfo.ConfirmedSiacoinBalance.Cmp(desiredbalance) > 0 {
			break
		}
		if time.Since(start) > miningTimeout {
			j.staticLogger.Errorf("%v: could not mine enough currency within %v timeout", j.staticDataDir, miningTimeout)
			return
		}
	}

	// Create a temporary folder for hosting if it does not exist. The folder
	// can exist when we are performing host upgrade and we are restarting its
	// jobHost after the ant upgrade.
	hostdir, err := filepath.Abs(filepath.Join(j.staticDataDir, "hostdata"))
	if err != nil {
		j.staticLogger.Errorf("%v: can't get hostdata directory absolute path: %v", j.staticDataDir, err)
		return
	}
	_, err = os.Stat(hostdir)
	if err != nil && !os.IsNotExist(err) {
		j.staticLogger.Errorf("%v: can't get hostdata directory info: %v", j.staticDataDir, err)
		return
	}
	// Folder doesn't exist
	if os.IsNotExist(err) {
		// Create a temporary folder for hosting
		err = os.MkdirAll(hostdir, 0700)
		if err != nil {
			j.staticLogger.Errorf("%v: can't create hostdata directory: %v", j.staticDataDir, err)
			return
		}

		// Add the storage folder.
		size := modules.SectorSize * 4096
		err = j.staticClient.HostStorageFoldersAddPost(hostdir, size)
		if err != nil {
			j.staticLogger.Errorf("%v: can't add storage folder: %v", j.staticDataDir, err)
			return
		}
	}

	// Accept contracts
	j.staticLogger.Debugf("%v: accept contracts", j.staticDataDir)
	err = j.staticClient.HostModifySettingPost(client.HostParamAcceptingContracts, true)
	if err != nil {
		j.staticLogger.Errorf("%v: can't accept contracts: %v", j.staticDataDir, err)
		return
	}

	// Announce host to the network, check periodically that host announcement
	// transaction is not re-orged, otherwise repeat. Check periodically that
	// storage revenue doesn't decrease.
	hjr, err := j.newHostJobRunner()
	if err != nil {
		j.staticLogger.Errorf("%v: can't create host job runner: %v", j.staticDataDir, err)
		return
	}
	for {
		// Return immediately when closing ant
		select {
		case <-j.StaticTG.StopChan():
			return
		default:
		}

		// Announce host to the network
		if !hjr.managedAnnounced() {
			// Announce host
			j.staticLogger.Debugf("%v: announce host", j.staticDataDir)
			err := j.staticClient.HostAnnouncePost()
			if err != nil {
				j.staticLogger.Errorf("%v: host announcement failed: %v", j.staticDataDir, err)
				select {
				case <-j.StaticTG.StopChan():
					return
				case <-time.After(hostAPIErrorFrequency):
					continue
				}
			}
			hjr.managedSetAnnounced(true)

			// Wait till host announcement transaction is in blockchain
			err = hjr.managedWaitAnnounceTransactionInBlockchain()
			if err != nil {
				j.staticLogger.Errorf("%v: waiting for host announcement transaction failed: %v", j.staticDataDir, err)
				hjr.managedSetAnnounced(false)
				continue
			}
		}

		// Check announce host transaction is not re-orged
		found, err := hjr.announcementTransactionInBlock(hjr.managedAnnouncedBlockHeight())
		if err != nil {
			j.staticLogger.Errorf("%v: checking host announcement transaction failed: %v", j.staticDataDir, err)
			select {
			case <-j.StaticTG.StopChan():
				return
			case <-time.After(hostAPIErrorFrequency):
				continue
			}
		}
		if !found {
			j.staticLogger.Debugf("%v: host announcement transaction was not found, it was probably re-orged", j.staticDataDir)
			hjr.managedSetAnnounced(false)
			continue
		}

		// Check storage revenue didn't decreased
		err = hjr.managedCheckStorageRevenueNotDecreased()
		if err != nil {
			j.staticLogger.Errorf("%v: checking storage revenue failed: %v", j.staticDataDir, err)
			select {
			case <-j.StaticTG.StopChan():
				return
			case <-time.After(hostAPIErrorFrequency):
				continue
			}
		}

		select {
		case <-j.StaticTG.StopChan():
			return
		case <-time.After(hostLoopFrequency):
		}
	}
}

// newHostJobRunner creates a new host specific hostJobRunner from generic
// jobRunner. hostJobRunner should be createad after host netAddress is
// possibly set for the host.
func (j *JobRunner) newHostJobRunner() (hostJobRunner, error) {
	hg, err := j.staticClient.HostGet()
	if err != nil {
		return hostJobRunner{}, errors.AddContext(err, "can't get host info")
	}
	na := hg.ExternalSettings.NetAddress
	return hostJobRunner{JobRunner: j, staticHostNetAddress: na}, nil
}

// announcementTransactionInBlock returns true if this host's host announcement
// transaction can be found in the given block.
func (hjr *hostJobRunner) announcementTransactionInBlock(blockHeight types.BlockHeight) (found bool, err error) {
	// Get blocks consensus with transactions
	cbg, err := hjr.staticClient.ConsensusBlocksHeightGet(blockHeight)
	if err != nil {
		return
	}

	// Check if transactions contain host announcement of this host
	for _, t := range cbg.Transactions {
		for _, arb := range t.ArbitraryData {
			addr, _, err := modules.DecodeAnnouncement(arb)
			if err != nil {
				continue
			}
			if addr == hjr.staticHostNetAddress {
				return true, nil
			}
		}
	}
	return
}

// managedAnnounced managed gets announced flag
func (hjr *hostJobRunner) managedAnnounced() bool {
	hjr.mu.Lock()
	defer hjr.mu.Unlock()
	return hjr.announced
}

// managedAnnouncedBlockHeight managed gets announcedBlockHeight
func (hjr *hostJobRunner) managedAnnouncedBlockHeight() types.BlockHeight {
	hjr.mu.Lock()
	defer hjr.mu.Unlock()
	return hjr.announcedBlockHeight
}

// managedAnnouncementTransactionInBlockRange managed updates
// announcedBlockHeight and returns true if a host announceent transaction was
// found in the given block range.
func (hjr *hostJobRunner) managedAnnouncementTransactionInBlockRange(start, end types.BlockHeight) (found bool, err error) {
	// Iterate through the blockchain
	for bh := start; bh <= end; bh++ {
		hjr.mu.Lock()
		found, err = hjr.announcementTransactionInBlock(bh)
		hjr.mu.Unlock()
		if err != nil {
			return
		}
		if found {
			hjr.mu.Lock()
			hjr.announcedBlockHeight = bh
			hjr.mu.Unlock()
			break
		}
	}
	return
}

// managedCheckStorageRevenueNotDecreased logs an error if the host's storage
// revenue decreases and managed updates host's last storage revenue.
func (hjr *hostJobRunner) managedCheckStorageRevenueNotDecreased() error {
	hostInfo, err := hjr.staticClient.HostGet()
	if err != nil {
		return err
	}

	// Print an error if storage revenue has decreased
	hjr.mu.Lock()
	r := hjr.lastStorageRevenue
	hjr.mu.Unlock()

	if hostInfo.FinancialMetrics.StorageRevenue.Cmp(r) < 0 {
		// Storage revenue has decreased!
		hjr.staticLogger.Errorf("%v: storage revenue decreased! Was %v, is now %v", hjr.staticDataDir, hjr.lastStorageRevenue, hostInfo.FinancialMetrics.StorageRevenue)
	}

	// Update previous revenue to new amount
	hjr.mu.Lock()
	hjr.lastStorageRevenue = hostInfo.FinancialMetrics.StorageRevenue
	hjr.mu.Unlock()

	return nil
}

// managedSetAnnounced managed sets announced flag
func (hjr *hostJobRunner) managedSetAnnounced(announced bool) {
	hjr.mu.Lock()
	defer hjr.mu.Unlock()
	hjr.announced = announced
}

// managedWaitAnnounceTransactionInBlockchain blocks till host announcement
// transaction appears in the blockchain
func (hjr *hostJobRunner) managedWaitAnnounceTransactionInBlockchain() error {
	var startBH types.BlockHeight
	for {
		// Get latest block height
		cg, err := hjr.staticClient.ConsensusGet()
		if err != nil {
			return err
		}
		currentBH := cg.Height

		// Set start block height for timeout
		if startBH == 0 {
			startBH = currentBH
		}

		// Iterate through the blockchain
		found, err := hjr.managedAnnouncementTransactionInBlockRange(types.BlockHeight(0), currentBH)
		if err != nil {
			select {
			case <-hjr.StaticTG.StopChan():
				return errAntStopped
			case <-time.After(hostAPIErrorFrequency):
				continue
			}
		}
		if found {
			hjr.staticLogger.Debugf("%v: host announcement transaction is in blockchain", hjr.staticDataDir)
			return nil
		}

		// Timeout waiting for host announcement transaction
		if currentBH > startBH+hostAnnounceBlockHeightDelay {
			er := fmt.Errorf("host announcement transaction was not found in blockchain within %v blocks, transaction was probably re-orged", hostAnnounceBlockHeightDelay)
			hjr.staticLogger.Debugf("%v: %v", hjr.staticDataDir, er)
			return er
		}

		// Wait for next iteration
		select {
		case <-hjr.StaticTG.StopChan():
			return errAntStopped
		case <-time.After(hostTransactionCheckFrequency):
			continue
		}
	}
}
