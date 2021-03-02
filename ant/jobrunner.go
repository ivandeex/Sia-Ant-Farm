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
	StaticWalletSeed string
	staticDataDir    string
	StaticTG         threadgroup.ThreadGroup
}

// newJobRunner creates a new job runner using the provided parameters. If the
// existingWalletSeed is empty, it expects the connected api to be newly
// initialized, and it will initialize a new wallet. If existingWalletSeed is
// set, it expects previous node directory structure including existing wallet.
// In both cases the wallet is unlocked for usage in the jobs. siadirectory is
// used in logging to identify the job runner.
func newJobRunner(logger *persist.Logger, ant *Ant, siadirectory string, existingWalletSeed string) (*JobRunner, error) {
	jr := &JobRunner{
		staticLogger:     logger,
		staticAntsSyncWG: ant.staticAntsSyncWG,
		staticAnt:        ant,
		staticClient:     ant.StaticClient,
		staticDataDir:    ant.Config.DataDir,
	}

	// Get the wallet
	wg, err := jr.staticClient.WalletGet()
	if err != nil {
		return nil, errors.AddContext(err, "can't get wallet info")
	}
	if wg.Unlocked && existingWalletSeed == "" {
		// Set the wallet seed in the jobrunner and return. This case happens
		// when newJobRunner() is called multiple times (by purpose or by
		// mistake) on the ant.
		wsg, err := jr.staticClient.WalletSeedsGet()
		if err != nil {
			return nil, errors.AddContext(err, "can't get wallet seeds")
		}
		jr.StaticWalletSeed = wsg.PrimarySeed
		return jr, nil
	}

	// Init the wallet when needed and save seed
	var checkSeed bool
	if existingWalletSeed == "" && !wg.Encrypted {
		// No wallet seed was specified and wallet is encrypted. Initialize a
		// new wallet.
		jr.staticLogger.Debugf("%v: init wallet", jr.staticDataDir)
		walletParams, err := jr.staticClient.WalletInitPost("", false)
		if err != nil {
			er := errors.AddContext(err, "can't init wallet")
			jr.staticLogger.Errorf("%v: %v", jr.staticDataDir, er)
			return nil, er
		}
		jr.StaticWalletSeed = walletParams.PrimarySeed
	} else if existingWalletSeed == "" && wg.Encrypted {
		// Nothing to do. Not sure if or when this case can happen.
	} else if existingWalletSeed != "" && !wg.Encrypted {
		// A wallet seed was specified, but wallet is not encrypted. Initialize
		// the wallet with the existing seed.
		jr.staticLogger.Debugf("%v: init wallet using existing seed", jr.staticDataDir)
		err := jr.staticClient.WalletInitSeedPost(existingWalletSeed, "", false)
		if err != nil {
			er := errors.AddContext(err, "can't init wallet using existing seed")
			jr.staticLogger.Errorf("%v: %v", jr.staticDataDir, er)
			return nil, er
		}
		jr.StaticWalletSeed = existingWalletSeed
	} else if existingWalletSeed != "" && wg.Encrypted {
		// A wallet seed was specified, wallet is encrypted. Just save seed.
		// Executed e.g. during siad upgrade with job runner re-creation.
		checkSeed = true
		jr.staticLogger.Debugf("%v: use existing initialized wallet", jr.staticDataDir)
		jr.StaticWalletSeed = existingWalletSeed
	}

	// Unlock the wallet
	err = jr.staticClient.WalletUnlockPost(jr.StaticWalletSeed)
	if err != nil {
		return nil, err
	}

	// Check that actual seed equals existingWalletSeed.
	if checkSeed {
		wsg, err := jr.staticClient.WalletSeedsGet()
		if err != nil {
			return nil, errors.AddContext(err, "can't get wallet seeds")
		}
		if wsg.PrimarySeed != existingWalletSeed {
			return nil, errors.New("wallet primary seed doesn't equal expected existing seed")
		}
	}

	return jr, nil
}

// Stop signals all running jobs to stop and blocks until the jobs have
// finished stopping.
func (j *JobRunner) Stop() error {
	err := j.StaticTG.Stop()
	if err != nil {
		return errors.AddContext(err, "can't stop thread group")
	}
	return nil
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
	newJR, err := newJobRunner(j.staticLogger, j.staticAnt, j.staticDataDir, j.StaticWalletSeed)
	if err != nil {
		return &JobRunner{}, errors.AddContext(err, "couldn't create an updated job runner")
	}

	return newJR, nil
}
