package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
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

	// antFarm defines the 'antfarm' type. antFarm orchestrates a collection of
	// ants and provides an API server to interact with them.
	antFarm struct {
		apiListener net.Listener

		// ants is a slice of Ants in this antfarm.
		ants []*ant.Ant

		// externalAnts is a slice of externally connected ants, that is, ants that
		// are connected to this antfarm but managed by another antfarm.
		externalAnts []*ant.Ant
		router       *httprouter.Router
	}
)

// createAntfarm creates a new antFarm given the supplied AntfarmConfig
func createAntfarm(config AntfarmConfig) (*antFarm, error) {
	// clear old antfarm data before creating an antfarm
	datadir := "./antfarm-data"
	if config.DataDir != "" {
		datadir = config.DataDir
	}

	os.RemoveAll(datadir)
	os.MkdirAll(datadir, 0700)

	farm := &antFarm{}

	// Set ants sync waitgroup
	if config.WaitForSync {
		ant.AntSyncWG.Add(1)
	}

	// Check whether UPnP is enabled on router
	upnprouter.CheckUPnPEnabled()

	// start up each ant process with its jobs
	ants, err := startAnts(config.AntConfigs...)
	if err != nil {
		return nil, errors.AddContext(err, "unable to start ants")
	}

	err = startJobs(ants...)
	if err != nil {
		return nil, errors.AddContext(err, "unable to start jobs")
	}

	farm.ants = ants
	defer func() {
		if err != nil {
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

// allAnts returns all ants, external and internal, associated with this
// antFarm.
func (af *antFarm) allAnts() []*ant.Ant {
	return append(af.ants, af.externalAnts...)
}

// connectExternalAntfarm connects the current antfarm to an external antfarm,
// using the antfarm api at externalAddress.
func (af *antFarm) connectExternalAntfarm(externalAddress string) error {
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
func (af *antFarm) ServeAPI() error {
	http.Serve(af.apiListener, af.router)
	return nil
}

// permanentSyncMonitor checks that all ants in the antFarm are on the same
// blockchain.
func (af *antFarm) permanentSyncMonitor() {
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
			log.Println("Ants are synchronized. Block Height: ", af.ants[0].BlockHeight())
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
func (af *antFarm) getAnts(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	err := json.NewEncoder(w).Encode(af.ants)
	if err != nil {
		http.Error(w, "error encoding ants", 500)
	}
}

// Close signals all the ants to stop and waits for them to return.
func (af *antFarm) Close() error {
	if af.apiListener != nil {
		af.apiListener.Close()
	}
	for _, ant := range af.ants {
		ant.Close()
	}
	return nil
}
