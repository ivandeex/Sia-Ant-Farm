package ant

import (
	"log"
	"time"
)

// blockMining indefinitely mines blocks.  If more than 100
// seconds passes before the wallet has received some amount of currency, this
// job will print an error.
func (j *jobRunner) blockMining() {
	j.staticTG.Add()
	defer j.staticTG.Done()

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
		case <-j.staticTG.StopChan():
			return
		case <-time.After(time.Second * 100):
		}

		walletInfo, err = j.staticClient.WalletGet()
		if err != nil {
			log.Printf("[%v blockMining ERROR]: %v\n", j.staticSiaDirectory, err)
		}
		if walletInfo.ConfirmedSiacoinBalance.Cmp(lastBalance) > 0 {
			log.Printf("[%v SUCCESS] blockMining job succeeded", j.staticSiaDirectory)
			lastBalance = walletInfo.ConfirmedSiacoinBalance
		} else {
			log.Printf("[%v blockMining ERROR]: it took too long to receive new funds in miner job\n", j.staticSiaDirectory)
		}
	}
}
