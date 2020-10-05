package ant

import (
	"fmt"
	"log"
	"time"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/persist"
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

		walletGet, err := j.staticClient.WalletGet()
		if err != nil {
			j.staticAnt.StaticAntsCommon.Logger.Println(persist.LogLevelError, persist.LogCallerAntLittleSupplier, j.staticAnt.Config.DataDir, fmt.Sprintf("can't get wallet info: %v", err))
			return
		}

		if walletGet.ConfirmedSiacoinBalance.Cmp(sendAmount) < 0 {
			continue
		}

		_, err = j.staticClient.WalletSiacoinsPost(sendAmount, sendAddress, false)
		if err != nil {
			log.Printf("[ERROR] [littleSupplier] [%v] Can't send Siacoins: %v\n", j.staticSiaDirectory, err)
		}
	}
}
