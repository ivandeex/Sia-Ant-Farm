package ant

import (
	"sync"
	"time"

	"gitlab.com/NebulousLabs/Sia/types"
	"gitlab.com/NebulousLabs/errors"
)

const (
	// balanceIncreaseCheckInterval is how often the wallet will be checke for
	// balance increases
	// xxx used?
	balanceIncreaseCheckInterval = time.Second * 100

	// xxx
	minerWaitForBalanceTimeout = time.Minute * 5

	// xxx
	minerWaitForBalanceFrequency = time.Second * 5

	// xxx
	minerAPIErrorFrequency = time.Second * 5
)

var (
	// ErrWaitForBalanceTimeout is returned when miner doesn't have enough
	// ballance to send a requested amount even after the given timeout.
	ErrWaitForBalanceTimeout = errors.New("timeout when waiting for a balance to reach amount to be sent")
)

// xxx doc
type minerJobRunner struct {
	*JobRunner
	sync.Locker
	lastWalletBallance types.Currency
	sentTotal          types.Currency
}

//xxx remove
// // blockMining indefinitely mines blocks.  If more than 100
// // seconds passes before the wallet has received some amount of currency, this
// // job will print an error.
// func (j *JobRunner) blockMiningxxx() {
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

// 	err = j.staticClient.MinerStartGet()
// 	if err != nil {
// 		j.staticLogger.Errorf("%v: can't start miner: %v", j.staticDataDir, err)
// 		return
// 	}

// 	walletInfo, err := j.staticClient.WalletGet()
// 	if err != nil {
// 		j.staticLogger.Errorf("%v: can't get wallet info: %v", j.staticDataDir, err)
// 		return
// 	}
// 	lastBalance := walletInfo.ConfirmedSiacoinBalance

// 	// Every 100 seconds, verify that the balance has increased.
// 	for {
// 		select {
// 		case <-j.StaticTG.StopChan():
// 			return
// 		case <-time.After(balanceIncreaseCheckInterval):
// 		}

// 		walletInfo, err = j.staticClient.WalletGet()
// 		if err != nil {
// 			j.staticLogger.Errorf("%v: can't get wallet info: %v", j.staticDataDir, err)
// 			continue
// 		}
// 		if walletInfo.ConfirmedSiacoinBalance.Cmp(lastBalance) > 0 {
// 			j.staticLogger.Printf("%v: Blockmining job succeeded", j.staticDataDir)
// 			lastBalance = walletInfo.ConfirmedSiacoinBalance
// 		} else {
// 			j.staticLogger.Errorf("%v: it took too long to receive new funds in miner job", j.staticDataDir)
// 		}
// 	}
// }

// blockMining indefinitely mines blocks.  If more than 100
// seconds passes before the wallet has received some amount of currency, this
// job will print an error. // xxx redoc
func (j *JobRunner) blockMining() {
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

	// Start miner
	err = j.staticClient.MinerStartGet()
	if err != nil {
		j.staticLogger.Errorf("%v: can't start miner: %v", j.staticDataDir, err)
		return
	}

	// Create miner job runner
	mjr := minerJobRunner{
		JobRunner:          j,
		lastWalletBallance: types.ZeroCurrency,
		sentTotal:          types.ZeroCurrency,
	}

	// Start miner balance checker and payment sender
	go mjr.ballanceChecker()
	go mjr.paymentSender()
}

//xxx doc
func (mjr *minerJobRunner) ballanceChecker() {
	// Every 100 seconds, verify that the wallet balance plus sent payments has
	// increased.
	for {
		select {
		case <-mjr.StaticTG.StopChan():
			return
		case <-time.After(balanceIncreaseCheckInterval):
		}

		walletInfo, err := mjr.staticClient.WalletGet()
		if err != nil {
			mjr.staticLogger.Errorf("%v: can't get wallet info: %v", mjr.staticDataDir, err)
			continue
		}
		// Compare wallet ballance and sent out payments with last wallet balance
		if walletInfo.ConfirmedSiacoinBalance.Add(mjr.sentTotal).Cmp(mjr.lastWalletBallance) > 0 {
			mjr.staticLogger.Printf("%v: blockmining job succeeded", mjr.staticDataDir)
			mjr.lastWalletBallance = walletInfo.ConfirmedSiacoinBalance
		} else {
			mjr.staticLogger.Errorf("%v: it took too long to receive new funds in miner job", mjr.staticDataDir)
		}
	}
}

// xxx doc
func (mjr *minerJobRunner) paymentSender() {
	// xxx doc
	// Wait till balance is high enough
	// Timeout
	// Send
	// Response

	for {
		var pr PaymentRequest
		select {
		case <-mjr.StaticTG.StopChan():
			return
		case pr = <-mjr.staticAnt.staticMinerPaymentRequestChan:
		}

		// Wait till wallet balance reaches desired amount
		var paymentResponse paymentResponse
		err := mjr.waitForBallance(pr.amount)
		if err != nil {
			paymentResponse.err = err
			pr.responseChan <- paymentResponse
		}

		// Send SiaCoins
		wsp, err := mjr.staticClient.WalletSiacoinsPost(pr.amount, pr.walletAddress, false)
		if err != nil {
			er := errors.AddContext(err, "miner can't send siacoins")
			mjr.staticLogger.Errorf("%v: %v", mjr.staticDataDir, er)
			paymentResponse.err = er
			pr.responseChan <- paymentResponse
		}

		// Update sent total
		mjr.sentTotal = mjr.sentTotal.Add(pr.amount)

		// Send confirmation response
		paymentResponse.transactionIDs = wsp.TransactionIDs
		pr.responseChan <- paymentResponse
	}
}

// xxx doc
func (mjr *minerJobRunner) waitForBallance(desiredBallance types.Currency) error {
	start := time.Now()
	for {
		// Timeout
		if time.Since(start) > minerWaitForBalanceTimeout {
			return ErrWaitForBalanceTimeout
		}

		// Get ballance
		walletInfo, err := mjr.staticClient.WalletGet()
		if err != nil {
			mjr.staticLogger.Errorf("%v: can't get wallet info: %v", mjr.staticDataDir, err)
			select {
			case <-mjr.StaticTG.StopChan():
				return nil
			case <-time.After(minerAPIErrorFrequency):
				continue
			}
		}

		// Check we have enough ballance (confirmed ballance + unconfirmed incoming - unconfirmed outgoing > desired balance)
		confirmedBallance := walletInfo.ConfirmedSiacoinBalance
		unconfirmedIncomingSiacoins := walletInfo.UnconfirmedIncomingSiacoins
		unconfirmedOutgoingSiacoins := walletInfo.UnconfirmedOutgoingSiacoins
		if confirmedBallance.Add(unconfirmedIncomingSiacoins).Cmp(desiredBallance.Add(unconfirmedOutgoingSiacoins)) > 0 {
			return nil
		}

		// Sleep and repeat
		select {
		case <-mjr.StaticTG.StopChan():
			return nil
		case <-time.After(minerWaitForBalanceFrequency):
		}
	}
}
