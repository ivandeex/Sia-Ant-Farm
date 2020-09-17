package ant

import (
	"log"
	"time"

	"gitlab.com/NebulousLabs/Sia/types"
)

const (
	// walletBalanceCheckInterval defines how often the balance maintainer's
	// loop checks the wallet balance
	walletBalanceCheckInterval = time.Second * 20

	// balanceMaintainerErrorSleepDuration defines how long the balance maintainer
	// will sleep after an error
	balanceMaintainerErrorSleepDuration = time.Second * 5
)

// balanceMaintainer mines when the balance is below desiredBalance. The miner
// is stopped if the balance exceeds the desired balance.
func (j *JobRunner) balanceMaintainer(desiredBalance types.Currency) {
	err := j.StaticTG.Add()
	if err != nil {
		return
	}
	defer j.StaticTG.Done()

	// Wait for ants to be synced if the wait group was set
	stopped := j.waitForAntsSync()
	if stopped {
		return
	}

	minerRunning := true
	err = j.staticClient.MinerStartGet()
	if err != nil {
		log.Printf("[%v balanceMaintainer ERROR]: %v\n", j.staticSiaDirectory, err)
		return
	}

	// Every 20 seconds, check if the balance has exceeded the desiredBalance. If
	// it has and the miner is running, the miner is throttled. If the desired
	// balance has not been reached and the miner is not running, the miner is
	// started.
	for {
		walletInfo, err := j.staticClient.WalletGet()
		if err != nil {
			log.Printf("[%v balanceMaintainer ERROR]: %v\n", j.staticSiaDirectory, err)
			select {
			case <-j.StaticTG.StopChan():
				return
			case <-time.After(balanceMaintainerErrorSleepDuration):
			}
			continue
		}

		haveDesiredBalance := walletInfo.ConfirmedSiacoinBalance.Cmp(desiredBalance) > 0
		if !minerRunning && !haveDesiredBalance {
			log.Printf("[%v balanceMaintainer INFO]: not enough currency, starting the miner\n", j.staticSiaDirectory)
			minerRunning = true
			if err = j.staticClient.MinerStartGet(); err != nil {
				log.Printf("[%v miner ERROR]: %v\n", j.staticSiaDirectory, err)
				select {
				case <-j.StaticTG.StopChan():
					return
				case <-time.After(balanceMaintainerErrorSleepDuration):
				}
				continue
			}
		} else if minerRunning && haveDesiredBalance {
			log.Printf("[%v balanceMaintainer INFO]: mined enough currency, stopping the miner\n", j.staticSiaDirectory)
			minerRunning = false
			if err = j.staticClient.MinerStopGet(); err != nil {
				log.Printf("[%v balanceMaintainer ERROR]: %v\n", j.staticSiaDirectory, err)
				select {
				case <-j.StaticTG.StopChan():
					return
				case <-time.After(balanceMaintainerErrorSleepDuration):
				}
				continue
			}
		}

		select {
		case <-j.StaticTG.StopChan():
			return
		case <-time.After(walletBalanceCheckInterval):
		}
	}
}
