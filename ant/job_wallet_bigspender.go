package ant

import (
	"log"
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
			log.Printf("[ERROR] [bigSpender] [%v] Can't get wallet info: %v\n", j.staticSiaDirectory, err)
			return
		}

		if walletGet.ConfirmedSiacoinBalance.Cmp(spendThreshold) < 0 {
			continue
		}

		log.Printf("[INFO] [bigSpender] [%v] Sending a large transaction\n", j.staticSiaDirectory)

		voidaddress := types.UnlockHash{}
		_, err = j.staticClient.WalletSiacoinsPost(spendThreshold, voidaddress, false)
		if err != nil {
			log.Printf("[ERROR] [bigSpender] [%v] Can't send Siacoins: %v\n", j.staticSiaDirectory, err)
			continue
		}

		log.Printf("[INFO] [bigSpender] [%v] Large transaction send successful\n", j.staticSiaDirectory)
	}
}
