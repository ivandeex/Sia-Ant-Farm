package antfarm

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/julienschmidt/httprouter"

	"go.sia.tech/sia-antfarm/ant"
	"go.sia.tech/sia-antfarm/persist"
	"go.sia.tech/sia-antfarm/upnprouter"
	"go.sia.tech/siad/types"
	"gitlab.com/NebulousLabs/errors"
)

const (
	// monitorFrequency defines how frequently to run permanentSyncMonitor
	monitorFrequency = time.Second * 20

	// antsSyncTimeout defines a timeout for all ants to sync
	antsSyncTimeout = time.Minute * 3

	// asicHardforkTimeout defines a timeout for waiting for ASIC hardfork
	// height, which is 20 blocks on dev binaries.
	asicHardforkTimeout = time.Minute * 3

	// antfarmLog defines antfarm log filename
	antfarmLog = "antfarm.log"
)

type (
	// AntfarmConfig contains the fields to parse and use to create a sia-antfarm.
	AntfarmConfig struct {
		ListenAddress string
		DataDir       string
		AntConfigs    []ant.AntConfig
		AutoConnect   bool
		WaitForSync   bool

		// ExternalFarms is a slice of net addresses representing the API
		// addresses of other antFarms to connect to.
		ExternalFarms []string
	}

	// AntFarm defines the 'antfarm' type. antFarm orchestrates a collection of
	// ants and provides an API server to interact with them.
	AntFarm struct {
		apiListener net.Listener
		dataDir     string

		// Ants is a slice of Ants in this antfarm.
		Ants []*ant.Ant

		// externalAnts is a slice of externally connected ants, that is, ants
		// that are connected to this antfarm but managed by another antfarm.
		externalAnts []*ant.Ant
		router       *httprouter.Router

		// antsSyncWG is a waitgroup to wait for ASIC hardfork height and for
		// all ants to be in sync. Then all non-mining ant jobs start. Mining
		// jobs do not wait for this sync.
		antsSyncWG sync.WaitGroup

		// logger is an antfarm logger. It is passed to ants to log to the same
		// logger.
		logger *persist.Logger
	}
)

// New creates a new antFarm given the supplied AntfarmConfig
func New(logger *persist.Logger, config AntfarmConfig) (*AntFarm, error) {
	// clear old antfarm data before creating an antfarm
	dataDir := "./antfarm-data"
	if config.DataDir != "" {
		dataDir = config.DataDir
	}

	err := os.RemoveAll(dataDir)
	if err != nil {
		return nil, errors.AddContext(err, "can't remove antfarm data directory")
	}
	err = os.MkdirAll(dataDir, 0700)
	if err != nil {
		return nil, errors.AddContext(err, "can't create antfarm data directory")
	}

	farm := &AntFarm{
		dataDir: dataDir,
		logger:  logger,
	}

	// Set ants sync waitgroup
	if config.WaitForSync {
		farm.antsSyncWG.Add(1)
		defer farm.antsSyncWG.Done()
	}

	// Check whether UPnP is enabled on router
	upnpStatus := upnprouter.CheckUPnPEnabled()
	farm.logger.Debugln(upnpStatus)

	// Check ant names are unique
	antNames := make(map[string]struct{})
	for _, ant := range config.AntConfigs {
		if ant.Name == "" {
			continue
		}
		_, ok := antNames[ant.Name]
		if ok {
			return farm, fmt.Errorf("ant name %v is not unique", ant.Name)
		}
		antNames[ant.Name] = struct{}{}
	}

	// Start up each ant process with its jobs
	ants, err := startAnts(&farm.antsSyncWG, farm.logger, config.AntConfigs...)
	if err != nil {
		return nil, errors.AddContext(err, "unable to start ants")
	}

	err = startJobs(&farm.antsSyncWG, ants...)
	if err != nil {
		return nil, errors.AddContext(err, "unable to start jobs")
	}

	farm.Ants = ants
	defer func() {
		if err != nil {
			closeErr := farm.Close()
			if closeErr != nil {
				farm.logger.Errorf("can't close antfarm: %v", err)
			}
		}
	}()

	// if the AutoConnect flag is set, use connectAnts to bootstrap the network.
	if config.AutoConnect {
		if err = ConnectAnts(ants...); err != nil {
			return nil, errors.AddContext(err, "unable to connect ants")
		}
	}
	// connect the external antFarms
	for _, address := range config.ExternalFarms {
		if err = farm.ConnectExternalAntfarm(address); err != nil {
			return nil, errors.AddContext(err, "unable to connect external ant farm")
		}
	}

	// start up the api server listener
	farm.apiListener, err = net.Listen("tcp", config.ListenAddress)
	if err != nil {
		return nil, errors.AddContext(err, fmt.Sprintf("unable to create TCP connection on %v", config.ListenAddress))
	}

	// construct the router and serve the API.
	farm.router = httprouter.New()
	farm.router.GET("/ants", farm.getAnts)

	// Wait for ASIC hardfork height and for all ants to sync
	if config.WaitForSync {
		// Wait for ASIC hardfork height
		logger.Debugf("%v: waiting for ASIC hardfork height...", dataDir)
		err = farm.Ants[0].WaitForBlockHeight(types.ASICHardforkHeight, asicHardforkTimeout, time.Second)
		if err != nil {
			er := fmt.Errorf("waiting for ASIC hardfork height reached %v timeout: %v", asicHardforkTimeout, err)
			logger.Debugf("%v: %v", dataDir, er)
			return nil, er
		}
		logger.Debugf("%v: waiting for ASIC hardfork height finished", dataDir)

		// Wait for all ants being synced
		err = farm.waitForAntsToSync(antsSyncTimeout)
		if err != nil {
			return nil, fmt.Errorf("waiting for ants to sync reached %v timeout: %v", antsSyncTimeout, err)
		}
	}

	return farm, nil
}

// NewAntfarmLogger creates a new antfarm logger
func NewAntfarmLogger(dataDir string) (*persist.Logger, error) {
	logPath := filepath.Join(dataDir, antfarmLog)
	logger, err := persist.NewFileLogger(logPath)
	if err != nil {
		return nil, errors.AddContext(err, "can't create antfarm logger")
	}
	return logger, nil
}

// allAnts returns all ants, external and internal, associated with this
// antFarm.
func (af *AntFarm) allAnts() []*ant.Ant {
	return append(af.Ants, af.externalAnts...)
}

// ConnectExternalAntfarm connects the current antfarm to an external antfarm,
// using the antfarm api at externalAddress.
func (af *AntFarm) ConnectExternalAntfarm(externalAddress string) error {
	res, err := http.DefaultClient.Get("http://" + externalAddress + "/ants")
	if err != nil {
		return err
	}
	defer func() {
		if err := res.Body.Close(); err != nil {
			af.logger.Errorf("can't close response body: %v", err)
		}
	}()

	var externalAnts []*ant.Ant
	err = json.NewDecoder(res.Body).Decode(&externalAnts)
	if err != nil {
		return err
	}
	af.externalAnts = append(af.externalAnts, externalAnts...)
	return ConnectAnts(af.allAnts()...)
}

// ServeAPI serves the antFarm's http API.
func (af *AntFarm) ServeAPI() error {
	return http.Serve(af.apiListener, af.router)
}

// GetAntByName return the ant with the given name. If there is no ant with the
// given name error is reported.
func (af *AntFarm) GetAntByName(name string) (foundAnt *ant.Ant, err error) {
	for _, a := range af.Ants {
		if a.Config.Name == name {
			return a, nil
		}
	}
	return &ant.Ant{}, fmt.Errorf("ant with name %v doesn't exist", name)
}

// PermanentSyncMonitor checks that all ants in the antFarm are on the same
// blockchain.
func (af *AntFarm) PermanentSyncMonitor() {
	// Every 20 seconds, list all consensus groups and display the block height.
	for {
		// TODO: antfarm struct should have a threadgroup to be able to pick up
		// stopchan signals
		time.Sleep(monitorFrequency)

		// Grab consensus groups
		groups, err := antConsensusGroups(af.allAnts()...)
		if err != nil {
			af.logger.Errorf("can't check sync status of antfarm: %v", err)
			continue
		}

		// Check if ants are synced
		if len(groups) == 1 {
			af.logger.Printf("ants are synchronized. Block Height: %v", af.Ants[0].BlockHeight())
			continue
		}

		// Log out information about the unsync ants
		msg := "Ants split into multiple groups.\n"
		for i, group := range groups {
			msg += fmt.Sprintf("\tGroup %d:\n", i+1)
			for _, a := range group {
				msg += fmt.Sprintf("\t\t%s\n", a.APIAddr)
			}
		}
		af.logger.Print(msg)
	}
}

// getAnts is a http handler that returns the ants currently running on the
// antfarm.
func (af *AntFarm) getAnts(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	err := json.NewEncoder(w).Encode(af.Ants)
	if err != nil {
		http.Error(w, "error encoding ants", 500)
	}
}

// Close signals all the ants to stop and waits for them to return.
func (af *AntFarm) Close() error {
	af.logger.Println("starting to close antfarm")
	if af.apiListener != nil {
		if err := af.apiListener.Close(); err != nil {
			af.logger.Errorf("can't close antfarm http API listener: %v", err)
		}
	}

	// Speed up closing ants by calling concurrent goroutines
	var antCloseWG sync.WaitGroup
	for _, a := range af.Ants {
		antCloseWG.Add(1)
		go func(a *ant.Ant) {
			err := a.Close()
			if err != nil {
				af.logger.Errorf("can't close ant %v: %v", a.Config.SiadConfig.DataDir, err)
			}
			antCloseWG.Done()
		}(a)
	}
	antCloseWG.Wait()

	return nil
}

// GetAntConfigIndexByName returns index of ant config in antfarm's AntConfigs
// by given ant name
func (afc *AntfarmConfig) GetAntConfigIndexByName(name string) (antConfigIndex int, err error) {
	for i, ac := range afc.AntConfigs {
		if ac.Name == name {
			return i, nil
		}
	}
	return 0, fmt.Errorf("ant with name %v doesn't exist", name)
}

// GetHostAntConfigIndices returns slice of indices of ant configs which have
// defined host job
func (afc *AntfarmConfig) GetHostAntConfigIndices() (antConfigIndices []int) {
	for i, ac := range afc.AntConfigs {
		for _, j := range ac.Jobs {
			if j == "host" {
				antConfigIndices = append(antConfigIndices, i)
				break
			}
		}
	}
	return antConfigIndices
}

// waitForAntsToSync waits for all ants to be synced with a given tmeout
func (af *AntFarm) waitForAntsToSync(timeout time.Duration) error {
	af.logger.Debugf("%v: waiting for all ants to sync...", af.dataDir)
	start := time.Now()
	for {
		// Check sync status
		groups, err := antConsensusGroups(af.Ants...)
		if err != nil {
			return errors.AddContext(err, "unable to get consensus groups")
		}

		// We have reached ants sync
		if len(groups) == 1 {
			break
		}

		// We have reached timeout
		if time.Since(start) > timeout {
			return fmt.Errorf("ants didn't synced within %v timeout", timeout)
		}

		// Wait for jobs stop, timout or sleep
		select {
		case <-af.Ants[0].Jr.StaticTG.StopChan():
			// Jobs were stopped, do not wait anymore
			return errors.New("jobs were stopped")
		case <-time.After(waitForAntsToSyncFrequency):
			// Continue waiting for sync after sleep
		}
	}
	af.logger.Debugf("%v: waiting for all ants to sync finished", af.dataDir)
	return nil
}
