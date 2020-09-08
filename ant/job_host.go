package ant

import (
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

// hostJobLoopParams defines parameters for main host job loop. Main reason for
// use of this struct is that we can pass parameters by reference and we will
// avoid linting errors or need for workarounds which we get when using and
// updating multiple parameters not wrapped in the struct.
// parameters passed by reference.
type hostJobLoopParams struct {
	toAnnounceAccept bool
	announcedTxID    types.TransactionID
	acceptedTxID     types.TransactionID
	prevRevenue      types.Currency
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
	params := hostJobLoopParams{
		toAnnounceAccept: true,
	}
	for {
		// Announce host to the network and accept contracts, wait till the
		// transactions are confirmed, record transaction IDs to check later
		// that the transactions are not re-orged
		if cont, ret := announceAndAccept(j, &params); cont {
			continue
		} else if ret {
			return
		}

		// Check announce host transaction is not re-orged
		if cont, ret := transactionReorged(j, params.announcedTxID, &params); cont {
			log.Printf("[INFO] [host] [%v] Announcing host transaction doesn't exist anymore, transaction was probably re-orged\n", j.staticSiaDirectory)
			continue
		} else if ret {
			return
		}

		// Check accept contracts transaction is not re-orged
		if cont, ret := transactionReorged(j, params.acceptedTxID, &params); cont {
			log.Printf("[INFO] [host] [%v] Accepting contracts transaction doesn't exist anymore, transaction was probably re-orged\n", j.staticSiaDirectory)
			continue
		} else if ret {
			return
		}

		// Check storage revenue didn't decreased
		if cont, ret := storageRevenueDecreased(j, &params.prevRevenue); cont {
			continue
		} else if ret {
			return
		}

		select {
		case <-j.StaticTG.StopChan():
			return
		case <-time.After(hostLoopFrequency):
		}
	}
}

// announceAndAccept returns if there are unconfirmed transactions, otherwise
// posts host announcement, posts accepting contracts, waits till the
// transactions are confirmed and updates both transaction IDs.
func announceAndAccept(j *JobRunner, loopControl *hostJobLoopParams) (continueLoop, returnCaller bool) {
	// Return if should not announce and accept
	if !loopControl.toAnnounceAccept {
		return
	}

	// Get starting transactions count so that later we know that new
	// transactions were added or dropped
	startUnconfirmedTxs, startConfirmedTxs, err := filteredTransactions(j.staticClient)
	if err != nil {
		log.Printf("[ERROR] [host] [%v] Can't get wallet transactions count: %v\n", j.staticSiaDirectory, err)
		select {
		case <-j.StaticTG.StopChan():
			return false, true
		case <-time.After(hostAPIErrorFrequency):
			return true, false
		}
	}
	startUnconfirmedTxsLen := len(startUnconfirmedTxs)
	startConfirmedTxsLen := len(startConfirmedTxs)

	// If there are unconfirmed transactions wait till they are
	// confirmed or dropped
	if startUnconfirmedTxsLen > 0 {
		log.Printf("[INFO] [host] [%v] Wait till there are no unconfirmed transactions\n", j.staticSiaDirectory)
		select {
		case <-j.StaticTG.StopChan():
			return false, true
		case <-time.After(hostTransactionCheckFrequency):
			return true, false
		}
	}

	// Announce host to the network
	log.Printf("[INFO] [host] [%v] Announce host\n", j.staticSiaDirectory)
	err = j.staticClient.HostAnnouncePost()
	if err != nil {
		log.Printf("[ERROR] [host] [%v] Can't post host announcement: %v\n", j.staticSiaDirectory, err)
		select {
		case <-j.StaticTG.StopChan():
			return false, true
		case <-time.After(hostAPIErrorFrequency):
			return true, false
		}
	}

	// Accept contracts
	log.Printf("[INFO] [host] [%v] Accept contracts\n", j.staticSiaDirectory)
	err = j.staticClient.HostModifySettingPost(client.HostParamAcceptingContracts, true)
	if err != nil {
		log.Printf("[ERROR] [host] [%v] Can't post to accept contracts: %v\n", j.staticSiaDirectory, err)
		select {
		case <-j.StaticTG.StopChan():
			return false, true
		case <-time.After(hostAPIErrorFrequency):
			return true, false
		}
	}

	loopControl.toAnnounceAccept = false

	// Wait till the transactions get confirmed or timed-out
	return waitTransactionsConfirmed(j, startConfirmedTxsLen, loopControl)
}

// filteredTransactions returns a slice of unconfirmed and a slice of confirmed
// wallet transactions that have format same as host announcement transactions.
func filteredTransactions(c *client.Client) (unconfirmedTransactions []modules.ProcessedTransaction, confirmedTransactions []modules.ProcessedTransaction, err error) {
	// Get all transactions
	wtg, err := c.WalletTransactionsGet(0, math.MaxInt64)
	if err != nil {
		return unconfirmedTransactions, confirmedTransactions, errors.AddContext(err, "can't get wallet transactions")
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

	return unconfirmedTransactions, confirmedTransactions, nil
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
func storageRevenueDecreased(j *JobRunner, prevRevenue *types.Currency) (continueLoop, returnCaller bool) {
	hostInfo, err := j.staticClient.HostGet()
	if err != nil {
		log.Printf("[ERROR] [host] [%v] Can't get host info: %v\n", j.staticSiaDirectory, err)
		select {
		case <-j.StaticTG.StopChan():
			return false, true
		case <-time.After(hostAPIErrorFrequency):
			return true, false
		}
	}

	// Print an error if storage revenue has decreased
	if hostInfo.FinancialMetrics.StorageRevenue.Cmp(*prevRevenue) >= 0 {
		// Update previous revenue to new amount
		*prevRevenue = hostInfo.FinancialMetrics.StorageRevenue
	} else {
		// Storage revenue has decreased!
		log.Printf("[ERROR] [host] [%v] StorageRevenue decreased! Was %v, is now %v\n", j.staticSiaDirectory, &prevRevenue, hostInfo.FinancialMetrics.StorageRevenue)
	}

	return
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

// transactionReorged checks if the transactions is re-orged, if so, it sets
// announce and accept flag to true.
func transactionReorged(j *JobRunner, txID types.TransactionID, params *hostJobLoopParams) (continueLoop, returnCaller bool) {
	exists, err := transactionxExists(j.staticClient, txID)
	if err != nil {
		log.Printf("[ERROR] [host] [%v] Can't check if transaction exists: %v\n", j.staticSiaDirectory, err)
		select {
		case <-j.StaticTG.StopChan():
			return false, true
		case <-time.After(hostAPIErrorFrequency):
			return true, false
		}
	}
	if !exists {
		params.toAnnounceAccept = true
		return true, false
	}

	return
}

// waitTransactionsConfirmed waits till announce host and accept contracts
// transactions are confirmed with a timeout defined by a block height
// interval.
func waitTransactionsConfirmed(j *JobRunner, startConfirmedTxsLen int, loopControl *hostJobLoopParams) (continueLoop, returnCaller bool) {
	var startBH types.BlockHeight
	var lastTxsLen int
	for {
		// Get current block height for timeout and set starting block
		// height in the first iteration
		cg, err := j.staticClient.ConsensusGet()
		if err != nil {
			log.Printf("[ERROR] [host] [%v] Can't get consensus info: %v\n", j.staticSiaDirectory, err)
			select {
			case <-j.StaticTG.StopChan():
				return false, true
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
			loopControl.toAnnounceAccept = true
			return true, false
		}

		// Get transactions
		unconfirmedTxs, confirmedTxs, err := filteredTransactions(j.staticClient)
		if err != nil {
			log.Printf("[ERROR] [host] [%v] Can't get transactions: %v\n", j.staticSiaDirectory, err)
			select {
			case <-j.StaticTG.StopChan():
				return false, true
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
				return false, true
			case <-time.After(hostTransactionCheckFrequency):
				continue
			}
		}

		// If a transaction was dropped (re-orged) then announce host
		// and accept contracts again
		if unconfirmedTxsLen+confirmedTxsLen < lastTxsLen {
			log.Printf("[INFO] [host] [%v] Announce host or accept contracts transaction was dropped, transaction was probably re-orged\n", j.staticSiaDirectory)
			loopControl.toAnnounceAccept = true
			return true, false
		}
		lastTxsLen = unconfirmedTxsLen + confirmedTxsLen

		// When transactions get confirmed get transaction IDs
		if confirmedTxsLen >= startConfirmedTxsLen+2 {
			log.Printf("[INFO] [host] [%v] Announce host and accept contract transactions were confirmed\n", j.staticSiaDirectory)
			loopControl.announcedTxID = confirmedTxs[confirmedTxsLen-2].TransactionID
			loopControl.acceptedTxID = confirmedTxs[confirmedTxsLen-1].TransactionID
			return
		}
	}
}
