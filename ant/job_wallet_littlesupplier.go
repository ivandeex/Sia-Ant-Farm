package ant

import (
	"time"

	"gitlab.com/NebulousLabs/Sia/types"
)

var (
	sendInterval = time.Second * 2
	sendAmount   = types.NewCurrency64(1000).Mul(types.SiacoinPrecision)
)

func (j *JobRunner) littleSupplier(sendAddress types.UnlockHash) {
	err := j.StaticTG.Add()
	if err != nil {
		j.staticLogger.Errorf("%v: can't add thread group: %v", j.staticDataDir, err)
		return
	}
	defer j.StaticTG.Done()

	// Wait for ants to be synced if the wait group was set
	synced := j.waitForAntsSync()
	if !synced {
		j.staticLogger.Errorf("%v: waiting for ants to sync failed", j.staticDataDir)
		return
	}

	for {
		select {
		case <-j.StaticTG.StopChan():
			return
		case <-time.After(sendInterval):
		}

		walletGet, err := j.staticClient.WalletGet()
		if err != nil {
			j.staticLogger.Errorf("%v: can't get wallet info: %v", j.staticDataDir, err)
			return
		}

		if walletGet.ConfirmedSiacoinBalance.Cmp(sendAmount) < 0 {
			continue
		}

		_, err = j.staticClient.WalletSiacoinsPost(sendAmount, sendAddress, false)
		if err != nil {
			j.staticLogger.Errorf("%v: can't send Siacoins: %v", j.staticDataDir, err)
		}
	}
}
