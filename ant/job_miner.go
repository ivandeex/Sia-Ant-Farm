package ant

import (
	"log"
	"time"
)

const (
	// balanceIncreaseCheckInterval is how often the wallet will be checke for
	// balance increases
	balanceIncreaseCheckInterval = time.Second * 100
)

// blockMining indefinitely mines blocks.  If more than 100
// seconds passes before the wallet has received some amount of currency, this
// job will print an error.
func (j *JobRunner) blockMining() {
	j.StaticTG.Add()
	defer j.StaticTG.Done()

	// Wait for ants to be synced if the wait group was set
	AntSyncWG.Wait()

	err := j.staticClient.MinerStartGet()
	if err != nil {
		log.Printf("[%v blockMining ERROR]: %v\n", j.staticSiaDirectory, err)
		return
	}

	walletInfo, err := j.staticClient.WalletGet()
	if err != nil {
		log.Printf("[%v blockMining ERROR]: %v\n", j.staticSiaDirectory, err)
		return
	}
	lastBalance := walletInfo.ConfirmedSiacoinBalance

	// Every 100 seconds, verify that the balance has increased.
	for {
		select {
		case <-j.StaticTG.StopChan():
			return
		case <-time.After(balanceIncreaseCheckInterval):
		}

		walletInfo, err = j.staticClient.WalletGet()
		if err != nil {
			log.Printf("[%v blockMining ERROR]: %v\n", j.staticSiaDirectory, err)
			continue
		}
		if walletInfo.ConfirmedSiacoinBalance.Cmp(lastBalance) > 0 {
			log.Printf("[%v SUCCESS] blockMining job succeeded", j.staticSiaDirectory)
			lastBalance = walletInfo.ConfirmedSiacoinBalance
		} else {
			log.Printf("[%v blockMining ERROR]: it took too long to receive new funds in miner job\n", j.staticSiaDirectory)
		}
	}
}
