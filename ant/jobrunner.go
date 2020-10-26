package ant

import (
	"sync"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/persist"
	"gitlab.com/NebulousLabs/Sia/node/api/client"
	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/NebulousLabs/threadgroup"
)

// A JobRunner is used to start up jobs on the running Sia node.
type JobRunner struct {
	// staticLogger defines a logger an ant's jobrunner should log to. Each
	// jobrunner log message should identify the ant by ant's siad dataDir.
	staticLogger *persist.Logger

	staticAntsSyncWG *sync.WaitGroup
	staticAnt        *Ant
	staticClient     *client.Client
	staticWalletSeed string
	staticDataDir    string
	StaticTG         threadgroup.ThreadGroup
}

// newJobRunner creates a new job runner using the provided parameters. If the
// existingWalletSeed is empty, it expects the connected api to be newly
// initialized, and it will initialize a new wallet. If existingWalletSeed is
// set, it expects previous node directory structure including existing wallet.
// In both cases the wallet is unlocked for usage in the jobs. siadirectory is
// used in logging to identify the job runner.
func newJobRunner(logger *persist.Logger, ant *Ant, apiaddr string, authpassword string, siadirectory string, existingWalletSeed string) (*JobRunner, error) {
	opt, err := client.DefaultOptions()
	if err != nil {
		return nil, errors.AddContext(err, "unable to get client options")
	}
	opt.Address = apiaddr
	opt.Password = authpassword
	c := client.New(opt)
	jr := &JobRunner{
		staticLogger:     logger,
		staticAntsSyncWG: ant.staticAntsSyncWG,
		staticAnt:        ant,
		staticClient:     c,
		staticDataDir:    ant.Config.DataDir,
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
	newJR, err := newJobRunner(j.staticLogger, j.staticAnt, j.staticAnt.APIAddr, j.staticAnt.Config.APIPassword, j.staticDataDir, j.staticWalletSeed)
	if err != nil {
		return &JobRunner{}, errors.AddContext(err, "couldn't create an updated job runner")
	}

	return newJR, nil
}
