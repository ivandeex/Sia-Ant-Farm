package ant

import (
	"gitlab.com/NebulousLabs/Sia/node/api/client"
	"gitlab.com/NebulousLabs/Sia/sync"
	"gitlab.com/NebulousLabs/errors"
)

// A JobRunner is used to start up jobs on the running Sia node.
type JobRunner struct {
	staticClient         *client.Client
	staticWalletPassword string
	staticSiaDirectory   string
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
