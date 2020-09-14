package ant

import (
	"fmt"
	"log"
	"math"
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
	// announce or host accept contracts transaction to apeear in confirmed
	// transactions. If the transaction doesn't appear within the interval, we
	// announce host or accept contracts again.
	hostAnnounceAcceptBlockHeightDelay = types.BlockHeight(20)

	// hostAPIErrorFrequency defines frequency at which we retry unsuccessful
	// API call.
	hostAPIErrorFrequency = time.Second * 5

	// hostTransactionCheckFrequency defines frequency at which we check
	// announce host or accept contracts unconfirmed and confirmed
	// transactions.
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

// transactions stores transaction IDs for main host job loop. Main reason for
// use of this struct is that we can pass parameters by reference and we will
// avoid linting errors or need for workarounds which we get when using and
// updating multiple parameters not wrapped in the struct.
type transactions struct {
	announcedTxID types.TransactionID
	acceptedTxID  types.TransactionID
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

	// Announce host to the network, post accepting contracts, check
	// periodically that announcing and accepting transaction are not re-orged,
	// otherwise repeat. Check periodically that storage revenue doesn't
	// decrease.
	var announced bool
	var storedTransactions transactions
	prevRevenue := types.ZeroCurrency
	for {
		// Return immediately when closing ant
		select {
		case <-j.StaticTG.StopChan():
			return
		default:
		}

		// Announce host to the network
		if !announced {
			// Wait till there are no unconfirmed transactions
			err := waitForNoUnconfirmedTransactions(j)
			if err != nil {
				continue
			}

			// Announce host and accept contracts
			startConfirmedTxsLen, err := announceAndAccept(j)
			if err != nil {
				select {
				case <-j.StaticTG.StopChan():
					return
				case <-time.After(hostAPIErrorFrequency):
					continue
				}
			}
			announced = true

			// Wait till the announcing host and accepting contracts are
			// confirmed
			err = waitTransactionsConfirmed(j, startConfirmedTxsLen, &storedTransactions)
			if err != nil {
				announced = false
				continue
			}
		}

		// Check announce host transaction is not re-orged
		reorged, err := transactionReorged(j, storedTransactions.announcedTxID, &storedTransactions)
		if err != nil {
			select {
			case <-j.StaticTG.StopChan():
				return
			case <-time.After(hostAPIErrorFrequency):
				continue
			}
		}
		if reorged {
			announced = false
			continue
		}

		// Check accept contracts transaction is not re-orged
		reorged, err = transactionReorged(j, storedTransactions.acceptedTxID, &storedTransactions)
		if err != nil {
			select {
			case <-j.StaticTG.StopChan():
				return
			case <-time.After(hostAPIErrorFrequency):
				continue
			}
		}
		if reorged {
			announced = false
			continue
		}

		// Check storage revenue didn't decreased
		err = storageRevenueDecreased(j, &prevRevenue)
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

// announceAndAccept announces host to the network, posts host accepting
// contracts and returns the number of confirmed transactions at the start
func announceAndAccept(j *JobRunner) (startConfirmedTxsLen int, err error) {
	// Get starting transactions count so that later we know that new
	// transactions were added or dropped
	_, startConfirmedTxs, err := filteredTransactions(j.staticClient)
	if err != nil {
		log.Printf("[ERROR] [host] [%v] Can't get wallet transactions count: %v\n", j.staticSiaDirectory, err)
		return 0, errAPIError
	}

	// Announce host to the network
	log.Printf("[INFO] [host] [%v] Announce host\n", j.staticSiaDirectory)
	err = j.staticClient.HostAnnouncePost()
	if err != nil {
		log.Printf("[ERROR] [host] [%v] Can't post host announcement: %v\n", j.staticSiaDirectory, err)
		return 0, errAPIError
	}

	// Accept contracts
	log.Printf("[INFO] [host] [%v] Accept contracts\n", j.staticSiaDirectory)
	err = j.staticClient.HostModifySettingPost(client.HostParamAcceptingContracts, true)
	if err != nil {
		log.Printf("[ERROR] [host] [%v] Can't post to accept contracts: %v\n", j.staticSiaDirectory, err)
		return 0, errAPIError
	}

	return len(startConfirmedTxs), nil
}

// filteredTransactions returns a slice of unconfirmed and a slice of confirmed
// wallet transactions that have format same as host announcement transactions.
func filteredTransactions(c *client.Client) (unconfirmedTransactions []modules.ProcessedTransaction, confirmedTransactions []modules.ProcessedTransaction, err error) {
	// Get all transactions
	wtg, err := c.WalletTransactionsGet(0, math.MaxInt64)
	if err != nil {
		err = errors.AddContext(err, "can't get wallet transactions")
		return
	}

	// Filter transactions
	for _, tx := range wtg.UnconfirmedTransactions {
		if isAnnnouncementTypeTransaction(tx) {
			unconfirmedTransactions = append(unconfirmedTransactions, tx)
		}
	}
	for _, tx := range wtg.ConfirmedTransactions {
		if isAnnnouncementTypeTransaction(tx) {
			confirmedTransactions = append(confirmedTransactions, tx)
		}
	}

	return 
}

// isAnnnouncementTypeTransaction returns true if the format of the transaction
// satisfies host announcement transaction.
func isAnnnouncementTypeTransaction(tx modules.ProcessedTransaction) bool {
	ins := tx.Inputs
	outs := tx.Outputs
	// Filter the transactions we want
	if len(ins) == 1 &&
		ins[0].FundType == types.SpecifierSiacoinInput &&
		len(outs) == 2 &&
		outs[0].FundType == types.SpecifierSiacoinOutput &&
		outs[1].FundType == types.SpecifierSiacoinOutput {
		return true
	}
	return false
}

// storageRevenueDecreased logs an error if the host's storage revenue
// decreases.
func storageRevenueDecreased(j *JobRunner, prevRevenue *types.Currency) error {
	hostInfo, err := j.staticClient.HostGet()
	if err != nil {
		log.Printf("[ERROR] [host] [%v] Can't get host info: %v\n", j.staticSiaDirectory, err)
		return errAPIError
	}

	// Print an error if storage revenue has decreased
	if hostInfo.FinancialMetrics.StorageRevenue.Cmp(*prevRevenue) >= 0 {
		// Update previous revenue to new amount
		*prevRevenue = hostInfo.FinancialMetrics.StorageRevenue
	} else {
		// Storage revenue has decreased!
		log.Printf("[ERROR] [host] [%v] StorageRevenue decreased! Was %v, is now %v\n", j.staticSiaDirectory, &prevRevenue, hostInfo.FinancialMetrics.StorageRevenue)
	}

	return nil
}

// transactionxExists returns true is a confirmed wallet transaction with the
// given ID exists.
func transactionxExists(c *client.Client, txID types.TransactionID) (bool, error) {
	_, cTxs, err := filteredTransactions(c)
	if err != nil {
		return false, errors.AddContext(err, "can't get filtered transactions")
	}
	for _, tx := range cTxs {
		if tx.TransactionID == txID {
			return true, nil
		}
	}
	return false, nil
}

// transactionReorged return true if the transaction was re-orged.
func transactionReorged(j *JobRunner, txID types.TransactionID, storedTransactions *transactions) (transactionReorged bool, err error) {
	exists, err := transactionxExists(j.staticClient, txID)
	if err != nil {
		log.Printf("[ERROR] [host] [%v] Can't check if transaction exists: %v\n", j.staticSiaDirectory, err)
		return false, errAPIError
	}
	return !exists, nil
}

// waitForNoUnconfirmedTransactions blocks until there are no unconfirmed
// transactions
func waitForNoUnconfirmedTransactions(j *JobRunner) error {
	for {
		unconfirmedTxs, _, err := filteredTransactions(j.staticClient)
		if err != nil {
			log.Printf("[ERROR] [host] [%v] Can't get wallet transactions count: %v\n", j.staticSiaDirectory, err)
			select {
			case <-j.StaticTG.StopChan():
				return errAntStopped
			case <-time.After(hostAPIErrorFrequency):
				return errors.AddContext(err, "can't get wallet transactions count")
			}
		}
		if len(unconfirmedTxs) == 0 {
			return nil
		}
		log.Printf("[INFO] [host] [%v] Wait till there are no unconfirmed transactions\n", j.staticSiaDirectory)
		select {
		case <-j.StaticTG.StopChan():
			return errAntStopped
		case <-time.After(hostTransactionCheckFrequency):
		}
	}
}

// waitTransactionsConfirmed waits till announce host and accept contracts
// transactions are confirmed with a timeout defined by a block height
// interval.
func waitTransactionsConfirmed(j *JobRunner, startConfirmedTxsLen int, storedTransactions *transactions) error {
	var startBH types.BlockHeight
	var lastTxsLen int
	for {
		// Return immediately when closing ant
		select {
		case <-j.StaticTG.StopChan():
			return errAntStopped
		default:
		}

		// Get current block height for timeout and set starting block height
		// in the first iteration
		cg, err := j.staticClient.ConsensusGet()
		if err != nil {
			log.Printf("[ERROR] [host] [%v] Can't get consensus info: %v\n", j.staticSiaDirectory, err)
			select {
			case <-j.StaticTG.StopChan():
				return errAntStopped
			case <-time.After(hostAPIErrorFrequency):
				continue
			}
		}
		bh := cg.Height
		if startBH == 0 {
			startBH = bh
		}

		// Timeout waiting for confirmed transaction
		if bh > startBH+hostAnnounceAcceptBlockHeightDelay {
			log.Printf("[INFO] [host] [%v] Announce host or accept contracts transaction was not confirmed within %v blocks, transaction was probably re-orged\n", j.staticSiaDirectory, hostAnnounceAcceptBlockHeightDelay)
			msg := fmt.Sprintf("announce host or accept contracts transaction was not confirmed within %v blocks, transaction was probably re-orged", hostAnnounceAcceptBlockHeightDelay)
			return errors.AddContext(err, msg)
		}

		// Get transactions
		unconfirmedTxs, confirmedTxs, err := filteredTransactions(j.staticClient)
		if err != nil {
			log.Printf("[ERROR] [host] [%v] Can't get transactions: %v\n", j.staticSiaDirectory, err)
			select {
			case <-j.StaticTG.StopChan():
				return errAntStopped
			case <-time.After(hostAPIErrorFrequency):
				continue
			}
		}
		unconfirmedTxsLen := len(unconfirmedTxs)
		confirmedTxsLen := len(confirmedTxs)

		// If there is an unconfirmed transaction keep waiting
		if unconfirmedTxsLen > 0 {
			select {
			case <-j.StaticTG.StopChan():
				return errAntStopped
			case <-time.After(hostTransactionCheckFrequency):
				continue
			}
		}

		// If a transaction was dropped (re-orged) return error
		if unconfirmedTxsLen+confirmedTxsLen < lastTxsLen {
			log.Printf("[INFO] [host] [%v] Announce host or accept contracts transaction was dropped, transaction was probably re-orged\n", j.staticSiaDirectory)
			return errors.New("announce host or accept contracts transaction was dropped, transaction was probably re-orged")
		}
		lastTxsLen = unconfirmedTxsLen + confirmedTxsLen

		// When transactions get confirmed get transaction IDs
		if confirmedTxsLen >= startConfirmedTxsLen+2 {
			log.Printf("[INFO] [host] [%v] Announce host and accept contract transactions were confirmed\n", j.staticSiaDirectory)
			storedTransactions.announcedTxID = confirmedTxs[confirmedTxsLen-2].TransactionID
			storedTransactions.acceptedTxID = confirmedTxs[confirmedTxsLen-1].TransactionID
			return nil
		}
	}
}
