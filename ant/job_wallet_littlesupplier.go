package ant

import (
	"log"
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
	AntSyncWG.Wait()

	for {
		select {
		case <-j.StaticTG.StopChan():
			return
		case <-time.After(sendInterval):
		}

		walletGet, err := j.staticClient.WalletGet()
		if err != nil {
			log.Printf("[%v jobSpender ERROR]: %v\n", j.staticSiaDirectory, err)
			return
		}

		if walletGet.ConfirmedSiacoinBalance.Cmp(sendAmount) < 0 {
			continue
		}

		_, err = j.staticClient.WalletSiacoinsPost(sendAmount, sendAddress, false)
		if err != nil {
			log.Printf("[%v jobSupplier ERROR]: %v\n", j.staticSiaDirectory, err)
		}
	}
}
