package ant

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/persist"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/upnprouter"
	"gitlab.com/NebulousLabs/Sia/types"
	"gitlab.com/NebulousLabs/errors"
)

const (
	// updateSiadWarmUpTime defines initial warm-up sleep time for an ant after
	// siad update
	updateSiadWarmUpTime = time.Second * 10
)

// AntConfig represents a configuration object passed to New(), used to
// configure a newly created Sia Ant.
type AntConfig struct {
	SiadConfig

	Name            string `json:",omitempty"`
	Jobs            []string
	DesiredCurrency uint64
}

// An Ant is a Sia Client programmed with network user stories. It executes
// these user stories and reports on their successfulness.
type Ant struct {
	staticAntsSyncWG *sync.WaitGroup

	// staticLogger defines a logger an ant should log to. Each ant log message
	// should identify the ant by ant's siad dataDir.
	staticLogger *persist.Logger

	APIAddr string
	RPCAddr string

	Config AntConfig

	siad *exec.Cmd
	Jr   *JobRunner

	// A variable to track which blocks + heights the sync detector has seen
	// for this ant. The map will just keep growing, but it shouldn't take up a
	// prohibitive amount of space.
	SeenBlocks map[types.BlockHeight]types.BlockID `json:"-"`
}

// clearPorts discovers the UPNP enabled router and clears the ports used by an
// ant before the ant is started.
func clearPorts(config AntConfig) error {
	// Resolve addresses to be cleared
	RPCAddr, err := net.ResolveTCPAddr("tcp", config.RPCAddr)
	if err != nil {
		return errors.AddContext(err, "can't resolve port")
	}

	hostAddr, err := net.ResolveTCPAddr("tcp", config.HostAddr)
	if err != nil {
		return errors.AddContext(err, "can't resolve port")
	}

	siaMuxAddr, err := net.ResolveTCPAddr("tcp", config.SiaMuxAddr)
	if err != nil {
		return errors.AddContext(err, "can't resolve port")
	}

	siaMuxWsAddr, err := net.ResolveTCPAddr("tcp", config.SiaMuxWsAddr)
	if err != nil {
		return errors.AddContext(err, "can't resolve port")
	}

	// Clear ports on the UPnP enabled router
	err = upnprouter.ClearPorts(RPCAddr, hostAddr, siaMuxAddr, siaMuxWsAddr)
	if err != nil {
		return errors.AddContext(err, "can't clear ports")
	}
	return nil
}

// New creates a new Ant using the configuration passed through `config`.
func New(antsSyncWG *sync.WaitGroup, logger *persist.Logger, config AntConfig) (*Ant, error) {
	// Create ant working dir if it doesn't exist
	// (e.g. ant farm deleted the whole farm dir)
	if _, err := os.Stat(config.DataDir); os.IsNotExist(err) {
		os.MkdirAll(config.DataDir, 0700)
	}

	// Unforward the ports required for this ant
	upnprouter.CheckUPnPEnabled()
	if upnprouter.UPnPEnabled {
		err := clearPorts(config)
		if err != nil {
			logger.Debugf("%v: can't clear upnp ports for ant: %v", config.DataDir, err)
		}
	}

	// Construct the ant's Siad instance
	siad, err := newSiad(config.SiadConfig)
	if err != nil {
		return nil, errors.AddContext(err, "unable to create new siad process")
	}

	// Ensure siad is always stopped if an error is returned.
	defer func() {
		if err != nil {
			stopSiad(config.APIAddr, config.APIPassword, siad.Process)
		}
	}()

	ant := &Ant{
		staticAntsSyncWG: antsSyncWG,
		staticLogger:     logger,
		APIAddr:          config.APIAddr,
		RPCAddr:          config.RPCAddr,
		Config:           config,
		SeenBlocks:       make(map[types.BlockHeight]types.BlockID),
		siad:             siad,
	}

	j, err := newJobRunner(logger, ant, config.APIAddr, config.APIPassword, config.SiadConfig.DataDir, "")
	if err != nil {
		return nil, errors.AddContext(err, "unable to crate jobrunner")
	}
	ant.Jr = j

	for _, job := range config.Jobs {
		ant.StartJob(antsSyncWG, job)
	}

	if config.DesiredCurrency != 0 {
		go j.balanceMaintainer(types.SiacoinPrecision.Mul64(config.DesiredCurrency))
	}

	return ant, nil
}

// SprintJSON is a wrapper for json.MarshalIndent
func SprintJSON(v interface{}) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return fmt.Sprintln(string(data)), nil
}

// BlockHeight returns the highest block height seen by the ant.
func (a *Ant) BlockHeight() types.BlockHeight {
	height := types.BlockHeight(0)
	for h := range a.SeenBlocks {
		if h > height {
			height = h
		}
	}
	return height
}

// Close releases all resources created by the ant, including the Siad
// subprocess.
func (a *Ant) Close() error {
	a.Jr.Stop()
	stopSiad(a.APIAddr, a.Config.APIPassword, a.siad.Process)
	return nil
}

// HasRenterTypeJob returns true if the ant has renter type of job (renter or
// autoRenter)
func (a *Ant) HasRenterTypeJob() bool {
	for _, jobName := range a.Config.Jobs {
		jobNameLower := strings.ToLower(jobName)
		if strings.Contains(jobNameLower, "renter") {
			return true
		}
	}
	return false
}

// StartJob starts the job indicated by `job` after an ant has been
// initialized. Arguments are passed to the job using args.
func (a *Ant) StartJob(antsSyncWG *sync.WaitGroup, job string, args ...interface{}) error {
	if a.Jr == nil {
		return errors.New("ant is not running")
	}

	switch job {
	case "miner":
		go a.Jr.blockMining()
	case "host":
		go a.Jr.jobHost()
	case "noAllowanceRenter":
		go a.Jr.renter(walletFull)
	case "renter":
		go a.Jr.renter(allowanceSet)
	case "autoRenter":
		go a.Jr.renter(backgroundJobsStarted)
	case "gateway":
		go a.Jr.gatewayConnectability()
	case "bigspender":
		go a.Jr.bigSpender()
	case "littlesupplier":
		go a.Jr.littleSupplier(args[0].(types.UnlockHash))
	default:
		return errors.New("no such job")
	}

	return nil
}

// UpdateSiad updates ant to use the given siad binary.
func (a *Ant) UpdateSiad(siadPath string) error {
	a.staticLogger.Debugf("%v: %v", a.Config.DataDir, "closing ant before siad update")

	// Stop ant
	err := a.Close()
	if err != nil {
		return errors.AddContext(err, "can't stop running ant")
	}

	// Update path to new siad binary
	a.Config.SiadConfig.SiadPath = siadPath

	// Construct the ant's Siad instance
	a.staticLogger.Printf("%v: starting new siad process using %v", a.Config.SiadConfig.DataDir, siadPath)
	siad, err := newSiad(a.Config.SiadConfig)
	if err != nil {
		return errors.AddContext(err, "unable to create new siad process")
	}
	a.staticLogger.Debugf("%v: sSiad process started", a.Config.SiadConfig.DataDir)

	// Ensure siad is always stopped if an error is returned.
	defer func() {
		if err != nil {
			stopSiad(a.Config.APIAddr, a.Config.APIPassword, siad.Process)
		}
	}()

	// Update ant's siad process
	a.siad = siad

	// Update ant with recreated newly initialized job runner after siad update
	jr, err := recreateJobRunner(a.Jr)
	if err != nil {
		return errors.AddContext(err, "can't update jobrunner after siad update")
	}
	a.Jr = jr

	// Give a new siad process some warm-up time
	a.staticLogger.Debugf("%v: siad warm-up...", a.Config.SiadConfig.DataDir)
	select {
	case <-a.Jr.StaticTG.StopChan():
		return nil
	case <-time.After(updateSiadWarmUpTime):
	}
	a.staticLogger.Debugf("%v: siad warm-up finished", a.Config.SiadConfig.DataDir)

	// Restart jobs
	a.staticLogger.Debugf("%v: restarting ant's jobs", a.Config.SiadConfig.DataDir)
	for _, job := range a.Config.Jobs {
		a.StartJob(a.Jr.staticAntsSyncWG, job)
	}

	// Start balance maintainer if desired currency was set
	if a.Config.DesiredCurrency > 0 {
		go a.Jr.balanceMaintainer(types.SiacoinPrecision.Mul64(a.Config.DesiredCurrency))
	}

	return nil
}

// WalletAddress returns a wallet address that this ant can receive coins on.
func (a *Ant) WalletAddress() (*types.UnlockHash, error) {
	if a.Jr == nil {
		return nil, errors.New("ant is not running")
	}

	addressGet, err := a.Jr.StaticClient.WalletAddressGet()
	if err != nil {
		return nil, err
	}

	return &addressGet.Address, nil
}
