package ant

import (
	"log"
	"time"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/persist"
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

	err = j.staticClient.MinerStartGet()
	if err != nil {
		// TODO: Will be changed to Errorf once NebulousLabs/log is updated
		j.staticLogger.Printf("%v %v: can't start miner: %v", persist.ErrorLogPrefix, j.staticDataDir, err)
		return
	}

	walletInfo, err := j.staticClient.WalletGet()
	if err != nil {
		log.Printf("[ERROR] [blockMining] [%v] Can't get wallet info: %v\n", j.staticDataDir, err)
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
			log.Printf("[ERROR] [blockMining] [%v] Can't get wallet info: %v\n", j.staticDataDir, err)
			continue
		}
		if walletInfo.ConfirmedSiacoinBalance.Cmp(lastBalance) > 0 {
			log.Printf("[INFO] [blockMining] [%v] Blockmining job succeeded\n", j.staticDataDir)
			lastBalance = walletInfo.ConfirmedSiacoinBalance
		} else {
			log.Printf("[ERROR] [blockMining] [%v] It took too long to receive new funds in miner job\n", j.staticDataDir)
		}
	}
}
