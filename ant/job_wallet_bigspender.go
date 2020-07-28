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
	j.StaticTG.Add()
	defer j.StaticTG.Done()

	// Wait for ants to be synced if the wait group was set
	AntSyncWG.Wait()

	for {
		select {
		case <-j.StaticTG.StopChan():
			return
		case <-time.After(spendInterval):
		}

		walletGet, err := j.staticClient.WalletGet()
		if err != nil {
			log.Printf("[%v jobSpender ERROR]: %v\n", j.StaticSiaDirectory, err)
			return
		}

		if walletGet.ConfirmedSiacoinBalance.Cmp(spendThreshold) < 0 {
			continue
		}

		log.Printf("[%v jobSpender INFO]: sending a large transaction\n", j.StaticSiaDirectory)

		voidaddress := types.UnlockHash{}
		_, err = j.staticClient.WalletSiacoinsPost(spendThreshold, voidaddress, false)
		if err != nil {
			log.Printf("[%v jobSpender ERROR]: %v\n", j.StaticSiaDirectory, err)
			continue
		}

		log.Printf("[%v jobSpender INFO]: large transaction send successful\n", j.StaticSiaDirectory)
	}
}
