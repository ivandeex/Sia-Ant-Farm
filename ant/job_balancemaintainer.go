package ant

import (
	"log"
	"time"

	"gitlab.com/NebulousLabs/Sia/types"
)

// balanceMaintainer mines when the balance is below desiredBalance. The miner
// is stopped if the balance exceeds the desired balance.
func (j *jobRunner) balanceMaintainer(desiredBalance types.Currency) {
	j.staticTG.Add()
	defer j.staticTG.Done()

	minerRunning := true
	err := j.staticClient.MinerStartGet()
	if err != nil {
		log.Printf("[%v balanceMaintainer ERROR]: %v\n", j.staticSiaDirectory, err)
		return
	}

	// Every 20 seconds, check if the balance has exceeded the desiredBalance. If
	// it has and the miner is running, the miner is throttled. If the desired
	// balance has not been reached and the miner is not running, the miner is
	// started.
	for {
		select {
		case <-j.staticTG.StopChan():
			return
		case <-time.After(time.Second * 20):
		}

		walletInfo, err := j.staticClient.WalletGet()
		if err != nil {
			log.Printf("[%v balanceMaintainer ERROR]: %v\n", j.staticSiaDirectory, err)
			return
		}

		haveDesiredBalance := walletInfo.ConfirmedSiacoinBalance.Cmp(desiredBalance) > 0
		if !minerRunning && !haveDesiredBalance {
			log.Printf("[%v balanceMaintainer INFO]: not enough currency, starting the miner\n", j.staticSiaDirectory)
			minerRunning = true
			if err = j.staticClient.MinerStartGet(); err != nil {
				log.Printf("[%v miner ERROR]: %v\n", j.staticSiaDirectory, err)
				return
			}
		} else if minerRunning && haveDesiredBalance {
			log.Printf("[%v balanceMaintainer INFO]: mined enough currency, stopping the miner\n", j.staticSiaDirectory)
			minerRunning = false
			if err = j.staticClient.MinerStopGet(); err != nil {
				log.Printf("[%v balanceMaintainer ERROR]: %v\n", j.staticSiaDirectory, err)
				return
			}
		}
	}
}
