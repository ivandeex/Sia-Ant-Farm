package ant

import (
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
		j.staticLogger.Errorf("%v: can't start miner: %v", j.staticDataDir, err)
		return
	}

	walletInfo, err := j.staticClient.WalletGet()
	if err != nil {
		j.staticLogger.Errorf("%v: can't get wallet info: %v", j.staticDataDir, err)
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
			j.staticLogger.Errorf("%v: can't get wallet info: %v", j.staticDataDir, err)
			continue
		}
		if walletInfo.ConfirmedSiacoinBalance.Cmp(lastBalance) > 0 {
			j.staticLogger.Printf("%v: Blockmining job succeeded", j.staticDataDir)
			lastBalance = walletInfo.ConfirmedSiacoinBalance
		} else {
			j.staticLogger.Errorf("%v: it took too long to receive new funds in miner job", j.staticDataDir)
		}
	}
}
