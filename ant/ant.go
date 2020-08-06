package ant

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/upnprouter"
	"gitlab.com/NebulousLabs/Sia/types"
	"gitlab.com/NebulousLabs/errors"
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
func New(antsSyncWG *sync.WaitGroup, config AntConfig) (*Ant, error) {
	// Create ant working dir if it doesn't exist
	// (e.g. ant farm deleted the whole farm dir)
	if _, err := os.Stat(config.DataDir); os.IsNotExist(err) {
		os.MkdirAll(config.DataDir, 0700)
	}

	// Unforward the ports required for this ant
	if upnprouter.UPnPEnabled {
		err := clearPorts(config)
		if err != nil {
			log.Printf("error clearing upnp ports for ant: %v\n", err)
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
			stopSiad(config.APIAddr, siad.Process)
		}
	}()

	ant := &Ant{
		APIAddr: config.APIAddr,
		RPCAddr: config.RPCAddr,
		Config:  config,

		siad: siad,

		SeenBlocks: make(map[types.BlockHeight]types.BlockID),
	}

	j, err := newJobRunner(antsSyncWG, ant, config.APIAddr, config.APIPassword, config.SiadConfig.DataDir)
	if err != nil {
		return nil, errors.AddContext(err, "unable to crate jobrunner")
	}
	ant.Jr = j

	for _, job := range config.Jobs {
		switch job {
		case "miner":
			go j.blockMining()
		case "host":
			go j.jobHost()
		case "renter":
			go j.renter(false)
		case "autoRenter":
			go j.renter(true)
		case "gateway":
			go j.gatewayConnectability()
		}
	}

	if config.DesiredCurrency != 0 {
		go j.balanceMaintainer(types.SiacoinPrecision.Mul64(config.DesiredCurrency))
	}

	return ant, nil
}

// PrintJSON is a wrapper for json.MarshalIndent
func PrintJSON(v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
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
	stopSiad(a.APIAddr, a.siad.Process)
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
	case "renter":
		go a.Jr.renter(false)
	case "autoRenter":
		go a.Jr.renter(true)
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

// WalletAddress returns a wallet address that this ant can receive coins on.
func (a *Ant) WalletAddress() (*types.UnlockHash, error) {
	if a.Jr == nil {
		return nil, errors.New("ant is not running")
	}

	addressGet, err := a.Jr.staticClient.WalletAddressGet()
	if err != nil {
		return nil, err
	}

	return &addressGet.Address, nil
}
