package ant

import (
	"time"

	"gitlab.com/NebulousLabs/Sia/types"
)

var (
	spendInterval  = time.Second * 30
	spendThreshold = types.NewCurrency64(5e4).Mul(types.SiacoinPrecision)
)

func (j *JobRunner) bigSpender() {
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
		case <-time.After(spendInterval):
		}

		walletGet, err := j.staticClient.WalletGet()
		if err != nil {
			j.staticLogger.Errorf("%v: can't get wallet info: %v", j.staticDataDir, err)
			return
		}

		if walletGet.ConfirmedSiacoinBalance.Cmp(spendThreshold) < 0 {
			continue
		}

		j.staticLogger.Debugf("%v: sending a large transaction", j.staticDataDir)

		voidaddress := types.UnlockHash{}
		_, err = j.staticClient.WalletSiacoinsPost(spendThreshold, voidaddress, false)
		if err != nil {
			j.staticLogger.Errorf("%v: can't send Siacoins: %v", j.staticDataDir, err)
			continue
		}

		j.staticLogger.Printf("%v: large transaction send successful", j.staticDataDir)
	}
}
