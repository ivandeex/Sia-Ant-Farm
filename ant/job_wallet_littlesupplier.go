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

func (j *jobRunner) littleSupplier(sendAddress types.UnlockHash) {
	j.staticTG.Add()
	defer j.staticTG.Done()

	for {
		select {
		case <-j.staticTG.StopChan():
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
