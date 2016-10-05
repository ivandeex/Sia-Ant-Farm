package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/NebulousLabs/Sia-Ant-Farm/ant"
	"github.com/NebulousLabs/Sia/api"
	"github.com/NebulousLabs/Sia/modules"
	"github.com/NebulousLabs/Sia/types"
)

// Ant defines the fields used by a Sia Ant.
/*
type Ant struct {
	APIAddr   string
	RPCAddr   string
	*exec.Cmd `json:"-"`

	// A variable to track which blocks + heights the sync detector has seen
	// for this ant. The map will just keep growing, but it shouldn't take up a
	// prohibitive amount of space.
	//	seenBlocks map[types.BlockHeight]types.BlockID
}
*/

// getAddrs returns n free listening ports by leveraging the
// behaviour of net.Listen(":0").  Addresses are returned in the format of
// ":port"
func getAddrs(n int) ([]string, error) {
	var addrs []string

	for i := 0; i < n; i++ {
		l, err := net.Listen("tcp", ":0")
		if err != nil {
			return nil, err
		}
		defer l.Close()
		addrs = append(addrs, fmt.Sprintf(":%v", l.Addr().(*net.TCPAddr).Port))
	}
	return addrs, nil
}

// connectAnts connects two or more ants to the first ant in the slice,
// effectively bootstrapping the antfarm.
func connectAnts(ants ...*ant.Ant) error {
	if len(ants) < 2 {
		return errors.New("you must call connectAnts with at least two ants.")
	}
	targetAnt := ants[0]
	c := api.NewClient(targetAnt.APIAddr, "")
	for _, ant := range ants[1:] {
		connectQuery := fmt.Sprintf("/gateway/connect/%v", ant.RPCAddr)
		addr := modules.NetAddress(ant.RPCAddr)
		if addr.Host() == "" {
			connectQuery = fmt.Sprintf("/gateway/connect/%v", "127.0.0.1"+ant.RPCAddr)
		}
		err := c.Post(connectQuery, "", nil)
		if err != nil {
			return err
		}
	}
	return nil
}

// antConsensusGroups iterates through all of the ants known to the antFarm
// and returns the different consensus groups that have been formed between the
// ants.
//
// The outer slice is the list of gorups, and the inner slice is a list of ants
// in each group.
/*
func antConsensusGroups(ants ...*Ant) (groups [][]*Ant, err error) {
	for _, ant := range ants {
		c := api.NewClient(ant.APIAddr, "")
		var cg api.ConsensusGET
		if err := c.Get("/consensus", &cg); err != nil {
			return nil, err
		}
		ant.seenBlocks[cg.Height] = cg.CurrentBlock

		// Compare this ant to all of the other groups. If the ant fits in a
		// group, insert it. If not, add it to the next group.
		found := false
		for gi, group := range groups {
			for i := types.BlockHeight(0); i < 8; i++ {
				id1, exists1 := ant.seenBlocks[cg.Height-i]
				id2, exists2 := group[0].seenBlocks[cg.Height-i] // no group should have a length of zero
				if exists1 && exists2 && id1 == id2 {
					groups[gi] = append(groups[gi], ant)
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			groups = append(groups, []*Ant{ant})
		}
	}
	return groups, nil
}
*/

// startAnts starts the ants defined by configs and blocks until every API
// has loaded.
func startAnts(configs ...AntConfig) ([]*ant.Ant, error) {
	var ants []*ant.Ant
	for i, config := range configs {
		cfg := parseConfig(config)
		fmt.Printf("[INFO] starting ant %v with config %v\n", i, cfg)
		ant, err := ant.New(cfg)
		if err != nil {
			return nil, err
		}
		ants = append(ants, ant)
	}

	return ants, nil
}

// parseConfig takes an input `config` and fills it with default values if
// required.
func parseConfig(config AntConfig) AntConfig {
	// if config.SiaDirectory isn't set, use ioutil.TempDir to create a new
	// temporary directory.
	if config.SiaDirectory == "" {
		tempdir, err := ioutil.TempDir("./antfarm-data", "ant")
		if err != nil {
			return nil, err
		}
		config.SiaDirectory = tempdir
	}

	// Automatically generate 3 free operating system ports for the Ant's api,
	// rpc, and host addresses
	addrs, err := getAddrs(3)
	if err != nil {
		return nil, err
	}
	if config.APIAddr == "" {
		config.APIAddr = addrs[0]
	}
	if config.RPCAddr == "" {
		config.RPCAddr = addrs[1]
	}
	if config.HostAddr == "" {
		config.HostAddr = addrs[2]
	}

	return config
}
