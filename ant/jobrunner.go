package ant

import (
	"fmt"
	"sync"
	"time"

	"gitlab.com/NebulousLabs/Sia/node/api/client"
	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/NebulousLabs/threadgroup"
)

// A JobRunner is used to start up jobs on the running Sia node.
type JobRunner struct {
	staticAntsSyncWG    *sync.WaitGroup
	staticAnt           *Ant
	staticClient        *client.Client
	staticWalletSeed    string
	staticSiaDirectory  string
	StaticTG            threadgroup.ThreadGroup
	renterUploadReadyWG sync.WaitGroup
}

// newJobRunner creates a new job runner using the provided parameters. If the
// existingWalletSeed is empty, it expects the connected api to be newly
// initialized, and it will initialize a new wallet. If existingWalletSeed is
// set, it expects previous node directory structure including existing wallet.
// In both cases the wallet is unlocked for usage in the jobs. siadirectory is
// used in logging to identify the job runner.
func newJobRunner(ant *Ant, apiaddr string, authpassword string, siadirectory string, existingWalletSeed string) (*JobRunner, error) {
	opt, err := client.DefaultOptions()
	if err != nil {
		return nil, errors.AddContext(err, "unable to get client options")
	}
	opt.Address = apiaddr
	opt.Password = authpassword
	c := client.New(opt)
	jr := &JobRunner{
		staticAntsSyncWG:   ant.StaticAntsCommon.AntsSyncWG,
		staticAnt:          ant,
		staticClient:       c,
		staticSiaDirectory: siadirectory,
	}
	if existingWalletSeed == "" {
		walletParams, err := jr.staticClient.WalletInitPost("", false)
		if err != nil {
			return nil, err
		}
		jr.staticWalletSeed = walletParams.PrimarySeed
	} else {
		jr.staticWalletSeed = existingWalletSeed
	}

	err = jr.staticClient.WalletUnlockPost(jr.staticWalletSeed)
	if err != nil {
		return nil, err
	}

	return jr, nil
}

// Stop signals all running jobs to stop and blocks until the jobs have
// finished stopping.
func (j *JobRunner) Stop() {
	j.StaticTG.Stop()
}

// waitForAntsSync returns true if wait has finished, false if jobRunner was
// stopped.
func (j *JobRunner) waitForAntsSync() bool {
	// Send antsSyncWG wait done to channel
	c := make(chan struct{})
	go func() {
		j.staticAntsSyncWG.Wait()
		c <- struct{}{}
	}()

	// Wait for antsSyncWG or stop channel
	select {
	case <-c:
		return true
	case <-j.StaticTG.StopChan():
		return false
	}
}

// recreateJobRunner creates a newly initialized job runner according to the
// given job runner
func recreateJobRunner(j *JobRunner) (*JobRunner, error) {
	// Create new job runner
	newJR, err := newJobRunner(j.staticAnt, j.staticAnt.APIAddr, j.staticAnt.Config.APIPassword, j.staticSiaDirectory, j.staticWalletSeed)
	if err != nil {
		return &JobRunner{}, errors.AddContext(err, "couldn't create an updated job runner")
	}

	return newJR, nil
}

// WaitForRenterUploadReady waits for renter upload ready with a given timeout
// if the ant has renter job. If the ant doesn't have renter job, it returns an
// error.
func (j *JobRunner) WaitForRenterUploadReady() error {
	if !j.staticAnt.HasRenterTypeJob() {
		return errors.New("this ant hasn't renter job")
	}
	// Wait till renter is upload ready or till timeout is reached
	ready := make(chan struct{})
	go func() {
		j.renterUploadReadyWG.Wait()
		close(ready)
	}()

	select {
	case <-ready:
		return nil
	case <-time.After(renterUploadReadyTimeout):
		return fmt.Errorf("waiting for renter to become upload ready reached timeout %v", renterUploadReadyTimeout)
	case <-j.StaticTG.StopChan():
		return errors.New("ant was stopped")
	}
}
