package ant

import (
	"fmt"
	"sync"
	"time"

	"gitlab.com/NebulousLabs/Sia/node/api/client"
	siasync "gitlab.com/NebulousLabs/Sia/sync"
	"gitlab.com/NebulousLabs/errors"
)

// A JobRunner is used to start up jobs on the running Sia node.
type JobRunner struct {
	staticAntsSyncWG     *sync.WaitGroup
	staticAnt            *Ant
	staticClient         *client.Client
	staticWalletPassword string
	staticSiaDirectory   string
	StaticTG             siasync.ThreadGroup
	renterUploadReadyWG  sync.WaitGroup
}

// newJobRunner creates a new job runner, using the provided api address,
// authentication password, and sia directory.  It expects the connected api to
// be newly initialized, and initializes a new wallet, for usage in the jobs.
// siadirectory is used in logging to identify the job runner.
func newJobRunner(antsSyncWG *sync.WaitGroup, ant *Ant, apiaddr string, authpassword string, siadirectory string) (*JobRunner, error) {
	opt, err := client.DefaultOptions()
	if err != nil {
		return nil, errors.AddContext(err, "unable to get client options")
	}
	opt.Address = apiaddr
	opt.Password = authpassword
	c := client.New(opt)
	jr := &JobRunner{
		staticAntsSyncWG:   antsSyncWG,
		staticAnt:          ant,
		staticClient:       c,
		staticSiaDirectory: siadirectory,
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

// Stop signals all running jobs to stop and blocks until the jobs have
// finished stopping.
func (j *JobRunner) Stop() {
	j.StaticTG.Stop()
}

// WaitForRenterUploadReady xxx
func (j *JobRunner) WaitForRenterUploadReady(timeout time.Duration) error {
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
	case <-time.After(timeout):
		return fmt.Errorf("waiting for upload ready reached timeout %v", timeout)
	case <-j.StaticTG.StopChan():
		return errors.New("ant was stopped")
	}
}
