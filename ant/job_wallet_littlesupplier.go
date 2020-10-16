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
		return
	}
	defer j.StaticTG.Done()

	// Wait for ants to be synced if the wait group was set
	synced := j.waitForAntsSync()
	if !synced {
		return
	}

	for {
		select {
		case <-j.StaticTG.StopChan():
			return
		case <-time.After(sendInterval):
		}

		walletGet, err := j.StaticClient.WalletGet()
		if err != nil {
			j.staticLogger.Errorf("%v: can't get wallet info: %v", j.staticDataDir, err)
			return
		}

		if walletGet.ConfirmedSiacoinBalance.Cmp(sendAmount) < 0 {
			continue
		}

		_, err = j.StaticClient.WalletSiacoinsPost(sendAmount, sendAddress, false)
		if err != nil {
			j.staticLogger.Errorf("%v: can't send Siacoins: %v", j.staticDataDir, err)
		}
	}
}
