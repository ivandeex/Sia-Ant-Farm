package ant

import (
	"log"
	"os"
	"path/filepath"
	"time"

	"gitlab.com/NebulousLabs/Sia/modules"
	"gitlab.com/NebulousLabs/Sia/node/api/client"
	"gitlab.com/NebulousLabs/Sia/types"
	"gitlab.com/NebulousLabs/errors"
)

const (
	// miningTimeout is timeout for mining desired balance
	miningTimeout = time.Minute * 5

	// miningSleepTime is sleep time for checking desired balance during mining
	miningSleepTime = time.Second

	// hostAnnouncementSleepTime is sleep time between host announcement tries
	hostAnnouncementSleepTime = time.Second * 5

	// hostSettingsPollSleepTime is sleep time for continuous host settings
	// polling
	hostSettingsPollSleepTime = time.Second * 15
)

// jobHost unlocks the wallet, mines some currency, and starts a host offering
// storage to the ant farm.
func (j *JobRunner) jobHost() {
	j.StaticTG.Add()
	defer j.StaticTG.Done()

	// Wait for ants to be synced if the wait group was set
	AntSyncWG.Wait()

	// Mine at least 50,000 SC
	desiredbalance := types.NewCurrency64(50000).Mul(types.SiacoinPrecision)
	success := false
	for start := time.Now(); time.Since(start) < miningTimeout; time.Sleep(miningSleepTime) {
		walletInfo, err := j.staticClient.WalletGet()
		if err != nil {
			log.Printf("[%v jobHost ERROR]: %v\n", j.staticSiaDirectory, err)
			return
		}
		if walletInfo.ConfirmedSiacoinBalance.Cmp(desiredbalance) > 0 {
			success = true
			break
		}
	}
	if !success {
		log.Printf("[%v jobHost ERROR]: timeout: could not mine enough currency after 5 minutes\n", j.staticSiaDirectory)
		return
	}

	// Create a temporary folder for hosting
	hostdir, _ := filepath.Abs(filepath.Join(j.staticSiaDirectory, "hostdata"))
	os.MkdirAll(hostdir, 0700)

	// Add the storage folder.
	size := modules.SectorSize * 4096
	err := j.staticClient.HostStorageFoldersAddPost(hostdir, size)
	if err != nil {
		log.Printf("[%v jobHost ERROR]: %v\n", j.staticSiaDirectory, err)
		return
	}

	// Announce the host to the network, retrying up to 5 times before reporting
	// failure and returning.
	success = false
	for try := 0; try < 5; try++ {
		err := j.staticClient.HostAnnouncePost()
		if err != nil {
			log.Printf("[%v jobHost ERROR]: %v\n", j.staticSiaDirectory, errors.AddContext(err, "host announcement not successful"))
		} else {
			success = true
			break
		}
		time.Sleep(hostAnnouncementSleepTime)
	}
	if !success {
		log.Printf("[%v jobHost ERROR]: could not announce after 5 tries.\n", j.staticSiaDirectory)
		return
	}
	log.Printf("[%v jobHost INFO]: successfully performed host announcement\n", j.staticSiaDirectory)

	// Accept contracts
	err = j.staticClient.HostModifySettingPost(client.HostParamAcceptingContracts, true)
	if err != nil {
		log.Printf("[%v jobHost ERROR]: %v\n", j.staticSiaDirectory, err)
		return
	}

	// Poll the API for host settings, logging them out with `INFO` tags.  If
	// `StorageRevenue` decreases, log an ERROR.
	maxRevenue := types.NewCurrency64(0)
	for {
		select {
		case <-j.StaticTG.StopChan():
			return
		case <-time.After(hostSettingsPollSleepTime):
		}

		hostInfo, err := j.staticClient.HostGet()
		if err != nil {
			log.Printf("[%v jobHost ERROR]: %v\n", j.staticSiaDirectory, err)
		}

		// Print an error if storage revenue has decreased
		if hostInfo.FinancialMetrics.StorageRevenue.Cmp(maxRevenue) >= 0 {
			maxRevenue = hostInfo.FinancialMetrics.StorageRevenue
		} else {
			// Storage revenue has decreased!
			log.Printf("[%v jobHost ERROR]: StorageRevenue decreased!  was %v is now %v\n", j.staticSiaDirectory, maxRevenue, hostInfo.FinancialMetrics.StorageRevenue)
		}
	}
}
