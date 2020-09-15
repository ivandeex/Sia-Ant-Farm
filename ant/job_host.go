package ant

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
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
	hostTransactionCheckFrequency = time.Millisecond * 500

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

	// errAPIError defines a reusable error when API call is not successful
	errAPIError = errors.New("error getting info through API")
)

// hostJobRunner extends generic jobRunner with host specific fields.
type hostJobRunner struct {
	*JobRunner
	announced            bool
	announcedBlockHeight types.BlockHeight
	lastStorageRevenue   types.Currency
}

// jobHost unlocks the wallet, mines some currency, and starts a host offering
// storage to the ant farm.
func (j *JobRunner) jobHost() {
	err := j.StaticTG.Add()
	if err != nil {
		return
	}
	defer j.StaticTG.Done()

	// Wait for ants to be synced if the wait group was set
	j.staticAntsSyncWG.Wait()

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
			log.Printf("[ERROR] [host] [%v] Error getting wallet info: %v\n", j.staticSiaDirectory, err)
			continue
		}
		if walletInfo.ConfirmedSiacoinBalance.Cmp(desiredbalance) > 0 {
			break
		}
		if time.Since(start) > miningTimeout {
			log.Printf("[ERROR] [host] [%v]: timeout: could not mine enough currency after 5 minutes\n", j.staticSiaDirectory)
			return
		}
	}

	// Create a temporary folder for hosting if it does not exist. The folder
	// can exist when we are performing host upgrade and we are restarting its
	// jobHost after the ant upgrade.
	hostdir, err := filepath.Abs(filepath.Join(j.staticSiaDirectory, "hostdata"))
	if err != nil {
		log.Printf("[ERROR] [jobHost] [%v] Can't get hostdata directory absolute path: %v\n", j.staticSiaDirectory, err)
		return
	}
	_, err = os.Stat(hostdir)
	if err != nil && !os.IsNotExist(err) {
		log.Printf("[ERROR] [jobHost] [%v] Can't get hostdata directory info: %v\n", j.staticSiaDirectory, err)
		return
	}
	// Folder doesn't exist
	if os.IsNotExist(err) {
		// Create a temporary folder for hosting
		err = os.MkdirAll(hostdir, 0700)
		if err != nil {
			log.Printf("[ERROR] [jobHost] [%v] Can't create hostdata directory: %v\n", j.staticSiaDirectory, err)
			return
		}

		// Add the storage folder.
		size := modules.SectorSize * 4096
		err = j.staticClient.HostStorageFoldersAddPost(hostdir, size)
		if err != nil {
			log.Printf("[%v jobHost ERROR]: %v\n", j.staticSiaDirectory, err)
			return
		}
	}

	// Accept contracts
	log.Printf("[INFO] [host] [%v] Accept contracts\n", j.staticSiaDirectory)
	err = j.staticClient.HostModifySettingPost(client.HostParamAcceptingContracts, true)
	if err != nil {
		log.Printf("[ERROR] [host] [%v] Can't post to accept contracts: %v\n", j.staticSiaDirectory, err)
		return
	}

	// Announce host to the network, check periodically that host announcement
	// transaction is not re-orged, otherwise repeat. Check periodically that
	// storage revenue doesn't decrease.
	hjr := j.newHostJobRunner()
	for {
		// Return immediately when closing ant
		select {
		case <-j.StaticTG.StopChan():
			return
		default:
		}

		// Announce host to the network
		if !hjr.announced {
			// Announce host
			err := hjr.announce()
			if err != nil {
				select {
				case <-j.StaticTG.StopChan():
					return
				case <-time.After(hostAPIErrorFrequency):
					continue
				}
			}
			hjr.announced = true

			// Wait till host announcement transaction is in blockchain
			err = hjr.waitAnnounceTransactionInBlockchain()
			if err != nil {
				hjr.announced = false
				continue
			}
		}

		// Check announce host transaction is not re-orged
		reorged, err := hjr.checkTransactionReorged()
		if err != nil {
			select {
			case <-j.StaticTG.StopChan():
				return
			case <-time.After(hostAPIErrorFrequency):
				continue
			}
		}
		if reorged {
			hjr.announced = false
			continue
		}

		// Check storage revenue didn't decreased
		err = hjr.checkStorageRevenueNotDecreased()
		if err != nil {
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
// jobRunner.
func (j *JobRunner) newHostJobRunner() hostJobRunner {
	return hostJobRunner{JobRunner: j}
}

// announce sets the number of confirmed transactions at the start and
// announces host to the network.
func (hjr *hostJobRunner) announce() error {
	// Announce host to the network
	log.Printf("[INFO] [host] [%v] Announce host\n", hjr.staticSiaDirectory)
	err := hjr.staticClient.HostAnnouncePost()
	if err != nil {
		log.Printf("[ERROR] [host] [%v] Can't post host announcement: %v\n", hjr.staticSiaDirectory, err)
		return errAPIError
	}

	return nil
}

// announcementTransactionInBlock returns true if this host's host announcement
// transaction can be found in the given block.
func (hjr *hostJobRunner) announcementTransactionInBlock(blockHeight types.BlockHeight) (found bool, err error) {
	// Get blocks consensus with transactions
	cbg, err := hjr.staticClient.ConsensusBlocksHeightGet(blockHeight)
	if err != nil {
		log.Printf("[ERROR] [host] [%v] Can't get consensus info: %v\n", hjr.staticSiaDirectory, err)
		return false, errAntStopped
	}

	// Check if transactions contain host announcement of this host
	for _, t := range cbg.Transactions {
		for _, arb := range t.ArbitraryData {
			addr, _, err := modules.DecodeAnnouncement(arb)
			if err != nil {
				continue
			}
			addrStr := fmt.Sprintf("%v:%v", addr.Host(), addr.Port())
			if addrStr == hjr.staticAnt.Config.HostAddr {
				return true, nil
			}
		}
	}
	return false, nil
}

// storageRevenueDecreased logs an error if the host's storage revenue
// decreases.
func (hjr *hostJobRunner) checkStorageRevenueNotDecreased() error {
	hostInfo, err := hjr.staticClient.HostGet()
	if err != nil {
		log.Printf("[ERROR] [host] [%v] Can't get host info: %v\n", hjr.staticSiaDirectory, err)
		return errAPIError
	}

	// Print an error if storage revenue has decreased
	if hostInfo.FinancialMetrics.StorageRevenue.Cmp(hjr.lastStorageRevenue) >= 0 {
		// Update previous revenue to new amount
		hjr.lastStorageRevenue = hostInfo.FinancialMetrics.StorageRevenue
	} else {
		// Storage revenue has decreased!
		log.Printf("[ERROR] [host] [%v] StorageRevenue decreased! Was %v, is now %v\n", hjr.staticSiaDirectory, hjr.lastStorageRevenue, hostInfo.FinancialMetrics.StorageRevenue)
	}

	return nil
}

// checkTransactionReorged return true when transaction was reorged and can't
// be found in the blockchain anymore.
func (hjr *hostJobRunner) checkTransactionReorged() (transactionReorged bool, err error) {
	found, err := hjr.announcementTransactionInBlock(hjr.announcedBlockHeight)
	if err != nil {
		log.Printf("[ERROR] [host] [%v] Can't get block transactions: %v\n", hjr.staticSiaDirectory, err)
		return false, errAPIError
	}
	if !found {
		log.Printf("[INFO] [host] [%v] Host announcement transaction was not found, it was probably re-orged\n", hjr.staticSiaDirectory)
	}
	return !found, nil
}

// currentBlockHeight returns current block height
func (hjr *hostJobRunner) currentBlockHeight() (types.BlockHeight, error) {
	cg, err := hjr.staticClient.ConsensusGet()
	if err != nil {
		log.Printf("[ERROR] [host] [%v] Can't get consensus info: %v\n", hjr.staticSiaDirectory, err)
		return 0, errAPIError
	}
	return cg.Height, nil
}

// waitAnnounceTransactionInBlockchain blocks till host announcement
// transaction appears in the blockchain
func (hjr *hostJobRunner) waitAnnounceTransactionInBlockchain() error {
	var startBH types.BlockHeight
	for {
		// Get latest block height
		latestBH, err := hjr.currentBlockHeight()
		if err != nil {
			return errAPIError
		}

		// Set start block height for timeout
		if startBH == 0 {
			startBH = latestBH
		}

		// Iterate through the blockchain
		for bh := types.BlockHeight(0); bh <= latestBH; bh++ {
			found, err := hjr.announcementTransactionInBlock(bh)
			if err != nil {
				select {
				case <-hjr.StaticTG.StopChan():
					return errAntStopped
				case <-time.After(hostAPIErrorFrequency):
					continue
				}
			}
			if found {
				log.Printf("[INFO] [host] [%v] Host announcement transaction is in block chain\n", hjr.staticSiaDirectory)
				hjr.announcedBlockHeight = bh
				return nil
			}
		}

		// Timeout waiting for host announcement transaction
		if latestBH > startBH+hostAnnounceBlockHeightDelay {
			log.Printf("[INFO] [host] [%v] Host announcement transaction was not found in blockchain within %v blocks, transaction was probably re-orged\n", hjr.staticSiaDirectory, hostAnnounceBlockHeightDelay)
			msg := fmt.Sprintf("host announcement transaction was not found in blockchain within %v blocks, transaction was probably re-orged", hostAnnounceBlockHeightDelay)
			return errors.AddContext(err, msg)
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
