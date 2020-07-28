package ant

import (
	"log"
	"time"

	"gitlab.com/NebulousLabs/Sia/node/api/client"
	"gitlab.com/NebulousLabs/Sia/sync"
	"gitlab.com/NebulousLabs/Sia/types"
	"gitlab.com/NebulousLabs/errors"
)

// A JobRunner is used to start up jobs on the running Sia node.
type JobRunner struct {
	staticClient         *client.Client
	staticWalletPassword string
	StaticSiaDirectory   string
	StaticTG             sync.ThreadGroup
}

// newJobRunner creates a new job runner, using the provided api address,
// authentication password, and sia directory.  It expects the connected api to
// be newly initialized, and initializes a new wallet, for usage in the jobs.
// siadirectory is used in logging to identify the job runner.
func newJobRunner(apiaddr string, authpassword string, siadirectory string) (*JobRunner, error) {
	opt, err := client.DefaultOptions()
	if err != nil {
		return nil, errors.AddContext(err, "unable to get client options")
	}
	opt.Address = apiaddr
	opt.Password = authpassword
	c := client.New(opt)
	jr := &JobRunner{
		staticClient:       c,
		StaticSiaDirectory: siadirectory,
	}
	walletParams, err := jr.staticClient.WalletInitPost("", false)
	if err != nil {
		return nil, err
	}
	jr.staticWalletPassword = walletParams.PrimarySeed

	err = jr.staticClient.WalletUnlockPost(jr.staticWalletPassword)
	if err != nil {
		return nil, err
	}

	return jr, nil
}

// BlockUntilWalletIsFilled blocks a job until wallet is filled with desired
// amount of SC
func (j *JobRunner) BlockUntilWalletIsFilled(jobName string, requiredBalance types.Currency, balanceCheckFrequency, balanceWarningTimeout time.Duration) {
	// Block until a minimum threshold of coins have been mined.
	start := time.Now()
	log.Printf("[INFO] [%v] [%v] Blocking until wallet is sufficiently full\n", jobName, j.StaticSiaDirectory)
	for {
		// Get the wallet balance.
		walletInfo, err := j.staticClient.WalletGet()
		if err != nil {
			log.Printf("[ERROR] [%v] [%v] Trouble when calling /wallet: %v\n", jobName, j.StaticSiaDirectory, err)

			// Wait before trying to get the balance again.
			select {
			case <-j.StaticTG.StopChan():
				return
			case <-time.After(balanceCheckFrequency):
			}
			continue
		}

		// Break the wait loop when we have enough balance.
		if walletInfo.ConfirmedSiacoinBalance.Cmp(requiredBalance) > 0 {
			break
		}

		// Log an error if the time elapsed has exceeded the warning threshold.
		if time.Since(start) > balanceWarningTimeout {
			log.Printf("[ERROR] [%v] [%v] Minimum balance for allowance has not been reached. Time elapsed: %v\n", jobName, j.StaticSiaDirectory, time.Since(start))
		}

		// Wait before trying to get the balance again.
		select {
		case <-j.StaticTG.StopChan():
			return
		case <-time.After(balanceCheckFrequency):
		}
	}
	log.Printf("[INFO] [%v] [%v] Wallet filled successfully.", jobName, j.StaticSiaDirectory)
}

// Stop signals all running jobs to stop and blocks until the jobs have
// finished stopping.
func (j *JobRunner) Stop() {
	j.StaticTG.Stop()
}
