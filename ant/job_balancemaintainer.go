package ant

import (
	"time"

	"gitlab.com/NebulousLabs/Sia/types"
)

const (
	// balanceMaintainerWalletCheckFrequency defines how often the balance maintainer's
	// loop checks the wallet balance
	// xxx used?
	balanceMaintainerWalletCheckFrequency = time.Second * 20

	// balanceMaintainerAPIErrorFrequency defines how long the balance
	// maintainer will sleep after an error
	balanceMaintainerAPIErrorFrequency = time.Second * 5

	// xxx
	balanceMaintainerPaymentErrorFrequency = time.Second * 5

	//xxx
	balanceMaintainerTransactionsConfirmationTimeout = time.Minute * 5 // xxx set to 3?

	// xxx
	balanceMaintainerConfirmationCheckFrequency = time.Second * 5

	//xxx
	requestDesiredBalanceRatio = 1.2
)

// PaymentRequest xxx doc
type PaymentRequest struct {
	amount        types.Currency
	walletAddress types.UnlockHash
	responseChan  chan paymentResponse
}

// xxx doc
type paymentResponse struct {
	err            error
	transactionIDs []types.TransactionID
}

//xxx remove
// // balanceMaintainer mines when the balance is below desiredBalance. The miner // xxx remove
// // is stopped if the balance exceeds the desired balance.
// func (j *JobRunner) balanceMaintainerxxx(desiredBalance types.Currency) {
// 	err := j.StaticTG.Add()
// 	if err != nil {
// 		return
// 	}
// 	defer j.StaticTG.Done()

// 	// Wait for ants to be synced if the wait group was set
// 	synced := j.waitForAntsSync()
// 	if !synced {
// 		return
// 	}

// 	minerRunning := true
// 	err = j.staticClient.MinerStartGet()
// 	if err != nil {
// 		j.staticLogger.Errorf("%v: can't start miner: %v", j.staticDataDir, err)
// 		return
// 	}

// 	// Every 20 seconds, check if the balance has exceeded the desiredBalance. If
// 	// it has and the miner is running, the miner is throttled. If the desired
// 	// balance has not been reached and the miner is not running, the miner is
// 	// started.
// 	for {
// 		walletInfo, err := j.staticClient.WalletGet()
// 		if err != nil {
// 			j.staticLogger.Errorf("%v: can't get wallet info: %v", j.staticDataDir, err)
// 			select {
// 			case <-j.StaticTG.StopChan():
// 				return
// 			case <-time.After(balanceMaintainerAPIErrorFrequency):
// 			}
// 			continue
// 		}

// 		haveDesiredBalance := walletInfo.ConfirmedSiacoinBalance.Cmp(desiredBalance) > 0
// 		if !minerRunning && !haveDesiredBalance {
// 			j.staticLogger.Printf("%v: not enough currency, starting the miner", j.staticDataDir)
// 			minerRunning = true
// 			if err = j.staticClient.MinerStartGet(); err != nil {
// 				j.staticLogger.Errorf("%v: can't start miner: %v", j.staticDataDir, err)
// 				select {
// 				case <-j.StaticTG.StopChan():
// 					return
// 				case <-time.After(balanceMaintainerAPIErrorFrequency):
// 				}
// 				continue
// 			}
// 		} else if minerRunning && haveDesiredBalance {
// 			j.staticLogger.Printf("%v: mined enough currency, stopping the miner", j.staticDataDir)
// 			minerRunning = false
// 			if err = j.staticClient.MinerStopGet(); err != nil {
// 				j.staticLogger.Errorf("%v: can't stop miner: %v", j.staticDataDir, err)
// 				select {
// 				case <-j.StaticTG.StopChan():
// 					return
// 				case <-time.After(balanceMaintainerAPIErrorFrequency):
// 				}
// 				continue
// 			}
// 		}

// 		select {
// 		case <-j.StaticTG.StopChan():
// 			return
// 		case <-time.After(balanceMaintainerWalletCheckFrequency):
// 		}
// 	}
// }

// balanceMaintainer mines when the balance is below desiredBalance. The miner // xxx update docs
// is stopped if the balance exceeds the desired balance.
func (j *JobRunner) balanceMaintainer(desiredBalance types.Currency) {
	err := j.StaticTG.Add()
	if err != nil {
		return
	}
	defer j.StaticTG.Done()

	// Wait for ants to be synced if the wait group was set
	synced := j.waitForAntsSync()
	if !synced {
		return
	}

	// Get ant's wallet address to send money to
	walletAddress, err := j.staticAnt.WalletAddress()
	if err != nil {
		j.staticLogger.Errorf("%v: can't get wallet address: %v", j.staticDataDir, err)
	}

	// Check continuously that ant has enough currency
balanceMaintainerLoop:
	for {
		walletInfo, err := j.staticClient.WalletGet()
		if err != nil {
			j.staticLogger.Errorf("%v: can't get wallet info: %v", j.staticDataDir, err)
			select {
			case <-j.StaticTG.StopChan():
				return
			case <-time.After(balanceMaintainerAPIErrorFrequency):
			}
			continue
		}

		haveDesiredBalance := walletInfo.ConfirmedSiacoinBalance.Cmp(desiredBalance) > 0

		if haveDesiredBalance {
			select {
			case <-j.StaticTG.StopChan():
				return
			case <-time.After(balanceMaintainerWalletCheckFrequency):
				continue
			}
		}

		// Ant doesn't have enough currency
		j.staticLogger.Printf("%v: not enough currency, sending payment request to miner", j.staticDataDir)

		// xxx ask for more defined by a ratio
		amount := desiredBalance.MulFloat(requestDesiredBalanceRatio).Sub(walletInfo.ConfirmedSiacoinBalance)
		responseChan := make(chan paymentResponse)
		paymentRequest := PaymentRequest{
			amount:        amount,
			walletAddress: *walletAddress,
			responseChan:  responseChan,
		}
		cg, err := j.staticClient.ConsensusGet()
		if err != nil {
			j.staticLogger.Errorf("%v: can't get consensus info: %v", j.staticDataDir, err)
			select {
			case <-j.StaticTG.StopChan():
				return
			case <-time.After(balanceMaintainerAPIErrorFrequency):
			}
			continue
		}
		startHeight := cg.Height
		j.staticLogger.Debugf("xxx %v: sending payment request for: %v", j.staticDataDir, paymentRequest.amount)
		j.staticAnt.staticMinerPaymentRequestChan <- paymentRequest
		j.staticLogger.Debugf("xxx %v: payment request received by miner", j.staticDataDir)

		// Check response to payment request
		var paymentResponse paymentResponse
		select {
		case <-j.StaticTG.StopChan():
			return
		case paymentResponse = <-responseChan:
		}

		// Process payment error from miner
		if err := paymentResponse.err; err != nil {
			j.staticLogger.Errorf("%v: didn't received payment from a miner: %v", j.staticDataDir, err)
			select {
			case <-j.StaticTG.StopChan():
				return
			case <-time.After(balanceMaintainerPaymentErrorFrequency):
				continue
			}
		}
		// Wait for transactions to become confirmed
		start := time.Now()
	waitForConfirmationLoop:
		for {
			// Timeout
			if time.Since(start) > balanceMaintainerTransactionsConfirmationTimeout {
				j.staticLogger.Errorf("%v: payment transactions were not confirmed within %v timeout", j.staticDataDir, balanceMaintainerTransactionsConfirmationTimeout)
				continue balanceMaintainerLoop
			}

			// Get current height
			cg, err := j.staticClient.ConsensusGet()
			if err != nil {
				j.staticLogger.Errorf("%v: can't get consensus info: %v", j.staticDataDir, err)
				select {
				case <-j.StaticTG.StopChan():
					return
				case <-time.After(balanceMaintainerAPIErrorFrequency):
					continue
				}
			}
			endHeight := cg.Height

			// Get confirmed transactions
			wtg, err := j.staticClient.WalletTransactionsGet(startHeight, endHeight)
			if err != nil {
				j.staticLogger.Errorf("%v: can't get wallet transactions: %v", j.staticDataDir, err)
				select {
				case <-j.StaticTG.StopChan():
					return
				case <-time.After(balanceMaintainerAPIErrorFrequency):
					continue
				}
			}

			// Check that payment transaction (one of the miner transactions in
			// the response) is confirmed
			for _, minerTxID := range paymentResponse.transactionIDs {
				for _, walletTx := range wtg.ConfirmedTransactions {
					if walletTx.TransactionID == minerTxID {
						// Payment transaction from miner got confirmed
						j.staticLogger.Debugf("xxx: %v: payment transaction confirmed", j.staticDataDir)
						break waitForConfirmationLoop
					}
				}
			}

			// Payment transaction is not yet between confirmed wallet transactions
			select {
			case <-j.StaticTG.StopChan():
				return
			case <-time.After(balanceMaintainerConfirmationCheckFrequency):
				continue waitForConfirmationLoop
			}
		}
	}
}
