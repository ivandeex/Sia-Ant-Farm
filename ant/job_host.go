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
	// hostAnnouncementFrequency defines how often the host will try to
	// announce itself
	hostAnnouncementFrequency = time.Second * 5

	// hostSettingsFrequency defines interval for continuous host settings
	// polling
	hostSettingsFrequency = time.Second * 15

	// miningTimeout defines timeout for mining desired balance
	miningTimeout = time.Minute * 5

	// miningCheckFrequency defines how often the host will check for desired
	// balance during mining
	miningCheckFrequency = time.Second
)

// jobHost unlocks the wallet, mines some currency, and starts a host offering
// storage to the ant farm.
func (j *JobRunner) jobHost() {
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

	// Mine at least 50,000 SC
	desiredbalance := types.NewCurrency64(50000).Mul(types.SiacoinPrecision)
	start := time.Now()
	for {
		select {
		case <-j.StaticTG.StopChan():
			return
		case <-time.After(miningCheckFrequency):
		}
		walletInfo, err := j.staticClient.WalletGet()
		if err != nil {
			log.Printf("[ERROR] [host] [%v] Error getting wallet info: %v\n", j.staticSiaDirectory, err)
			continue
		}
		if walletInfo.ConfirmedSiacoinBalance.Cmp(desiredbalance) > 0 {
			break
		}
		if time.Since(start) > miningTimeout {
			log.Printf("[ERROR] [host] [%v]: timeout: could not mine enough currency after 5 minutes\n", j.staticSiaDirectory)
			return
		}
	}

	// Create a temporary folder for hosting if it does not exist. The folder
	// can exist when we are performing host upgrade and we are restarting its
	// jobHost after the ant upgrade.
	hostdir, err := filepath.Abs(filepath.Join(j.staticSiaDirectory, "hostdata"))
	if err != nil {
		log.Printf("[ERROR] [jobHost] [%v] Can't get hostdata directory absolute path: %v\n", j.staticSiaDirectory, err)
		return
	}
	_, err = os.Stat(hostdir)
	if err != nil && !os.IsNotExist(err) {
		log.Printf("[ERROR] [jobHost] [%v] Can't get hostdata directory info: %v\n", j.staticSiaDirectory, err)
		return
	}
	// Folder doesn't exist
	if os.IsNotExist(err) {
		// Create a temporary folder for hosting
		err = os.MkdirAll(hostdir, 0700)
		if err != nil {
			log.Printf("[ERROR] [jobHost] [%v] Can't create hostdata directory: %v\n", j.staticSiaDirectory, err)
			return
		}

		// Add the storage folder.
		size := modules.SectorSize * 4096
		err = j.staticClient.HostStorageFoldersAddPost(hostdir, size)
		if err != nil {
			log.Printf("[%v jobHost ERROR]: %v\n", j.staticSiaDirectory, err)
			return
		}
	}

	// Announce the host to the network, retrying up to 5 times before reporting
	// failure and returning.
	success := false
	for try := 0; try < 5; try++ {
		err := j.staticClient.HostAnnouncePost()
		if err != nil {
			log.Printf("[%v jobHost ERROR]: %v\n", j.staticSiaDirectory, errors.AddContext(err, "host announcement not successful"))
		} else {
			success = true
			break
		}
		time.Sleep(hostAnnouncementFrequency)
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
		case <-time.After(hostSettingsFrequency):
		}

		hostInfo, err := j.staticClient.HostGet()
		if err != nil {
			log.Printf("[%v jobHost ERROR]: %v\n", j.staticSiaDirectory, err)
			continue
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
