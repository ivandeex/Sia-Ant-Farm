package antfarm

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/julienschmidt/httprouter"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/ant"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/upnprouter"
	"gitlab.com/NebulousLabs/errors"
)

const (
	// monitorFrequency defines how frequently to run permanentSyncMonitor
	monitorFrequency = time.Second * 20

	// antsSyncTimeout is a timeout for all ants to sync
	antsSyncTimeout = time.Minute * 5
)

type (
	// AntfarmConfig contains the fields to parse and use to create a sia-antfarm.
	AntfarmConfig struct {
		ListenAddress string
		DataDir       string
		AntConfigs    []ant.AntConfig
		AutoConnect   bool
		WaitForSync   bool

		// ExternalFarms is a slice of net addresses representing the API addresses
		// of other antFarms to connect to.
		ExternalFarms []string
	}

	// AntFarm defines the 'antfarm' type. antFarm orchestrates a collection of
	// ants and provides an API server to interact with them.
	AntFarm struct {
		apiListener net.Listener

		// Ants is a slice of Ants in this antfarm.
		Ants []*ant.Ant

		// externalAnts is a slice of externally connected ants, that is, ants that
		// are connected to this antfarm but managed by another antfarm.
		externalAnts []*ant.Ant
		router       *httprouter.Router

		// antsSyncWG is a waitgroup to wait for all ants to be in sync and then
		// start ant jobs
		antsSyncWG sync.WaitGroup
	}
)

// New creates a new antFarm given the supplied AntfarmConfig
func New(config AntfarmConfig) (farm *AntFarm, returnErr error) {
	// clear old antfarm data before creating an antfarm
	datadir := "./antfarm-data"
	if config.DataDir != "" {
		datadir = config.DataDir
	}

	os.RemoveAll(datadir)
	os.MkdirAll(datadir, 0700)

	farm = &AntFarm{}

	// Set ants sync waitgroup
	if config.WaitForSync {
		farm.antsSyncWG.Add(1)
		defer farm.antsSyncWG.Done()
	}

	// Check whether UPnP is enabled on router
	upnprouter.CheckUPnPEnabled()

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
	ants, err := startAnts(&farm.antsSyncWG, config.AntConfigs...)
	if err != nil {
		return nil, errors.AddContext(err, "unable to start ants")
	}

	err = startJobs(&farm.antsSyncWG, ants...)
	if err != nil {
		return nil, errors.AddContext(err, "unable to start jobs")
	}

	farm.Ants = ants
	defer func() {
		if returnErr != nil {
			farm.Close()
		}
	}()

	// if the AutoConnect flag is set, use connectAnts to bootstrap the network.
	if config.AutoConnect {
		if err = connectAnts(ants...); err != nil {
			return nil, errors.AddContext(err, "unable to connect ants")
		}
	}
	// connect the external antFarms
	for _, address := range config.ExternalFarms {
		if err = farm.connectExternalAntfarm(address); err != nil {
			return nil, errors.AddContext(err, "unable to connect external ant farm")
		}
	}

	// Wait for all ants to sync
	if config.WaitForSync {
		err = waitForAntsToSync(antsSyncTimeout, ants...)
		if err != nil {
			return nil, errors.AddContext(err, "wait for ants to sync failed")
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

	return farm, nil
}

// waitForAntsToSync waits for all ants to be synced with a given tmeout
func waitForAntsToSync(timeout time.Duration, ants ...*ant.Ant) error {
	log.Println("[INFO] [ant-farm] waiting for all ants to sync")
	start := time.Now()
	for {
		// Check sync status
		groups, err := antConsensusGroups(ants...)
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
		case <-ants[0].Jr.StaticTG.StopChan():
			// Jobs were stopped, do not wait anymore
			return errors.New("jobs were stopped")
		case <-time.After(waitForAntsToSyncFrequency):
			// Continue waiting for sync after sleep
		}
	}
	log.Println("[INFO] [ant-farm] all ants are now synced")
	return nil
}

// allAnts returns all ants, external and internal, associated with this
// antFarm.
func (af *AntFarm) allAnts() []*ant.Ant {
	return append(af.Ants, af.externalAnts...)
}

// connectExternalAntfarm connects the current antfarm to an external antfarm,
// using the antfarm api at externalAddress.
func (af *AntFarm) connectExternalAntfarm(externalAddress string) error {
	res, err := http.DefaultClient.Get("http://" + externalAddress + "/ants")
	if err != nil {
		return err
	}
	defer res.Body.Close()

	var externalAnts []*ant.Ant
	err = json.NewDecoder(res.Body).Decode(&externalAnts)
	if err != nil {
		return err
	}
	af.externalAnts = append(af.externalAnts, externalAnts...)
	return connectAnts(af.allAnts()...)
}

// ServeAPI serves the antFarm's http API.
func (af *AntFarm) ServeAPI() error {
	http.Serve(af.apiListener, af.router)
	return nil
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
			log.Println("error checking sync status of antfarm: ", err)
			continue
		}

		// Check if ants are synced
		if len(groups) == 1 {
			log.Println("Ants are synchronized. Block Height: ", af.Ants[0].BlockHeight())
			continue
		}

		// Log out information about the unsync ants
		log.Println("Ants split into multiple groups.")
		for i, group := range groups {
			if i != 0 {
				log.Println()
			}
			log.Println("Group ", i+1)
			for _, a := range group {
				log.Println(a.APIAddr)
			}
		}
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
	if af.apiListener != nil {
		af.apiListener.Close()
	}

	// Speed up closing ants by calling concurrent goroutines
	var antCloseWG sync.WaitGroup
	for _, a := range af.Ants {
		antCloseWG.Add(1)
		go func(a *ant.Ant) {
			err := a.Close()
			if err != nil {
				log.Printf("[ERROR] [ant] [%v] Error closing ant: %v\n", a.Config.SiadConfig.DataDir, err)
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
