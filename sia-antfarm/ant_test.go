package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/ant"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/test"
	"gitlab.com/NebulousLabs/Sia/node/api/client"
)

// TestStartAnts verifies that startAnts successfully starts ants given some
// configs.
func TestStartAnts(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create minimum configs
	dataDir := test.TestDir(t.Name())
	antDirs := []string{}
	for i := 0; i < 3; i++ {
		path := filepath.Join(dataDir, strconv.Itoa(i))
		antDirs = append(antDirs, path)

		// Clean ant dirs
		os.RemoveAll(path)
		os.MkdirAll(path, 0700)
	}
	configs := []ant.AntConfig{
		{
			SiadConfig: ant.SiadConfig{
				DataDir:  antDirs[0],
				SiadPath: test.TestSiadPath,
			},
		},
		{
			SiadConfig: ant.SiadConfig{
				DataDir:  antDirs[1],
				SiadPath: test.TestSiadPath,
			},
		},
		{
			SiadConfig: ant.SiadConfig{
				DataDir:  antDirs[2],
				SiadPath: test.TestSiadPath,
			},
		},
	}

	// Start ants
	ants, err := startAnts(configs...)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, ant := range ants {
			ant.Close()
		}
	}()

	// verify each ant has a reachable api
	for _, ant := range ants {
		opts, err := client.DefaultOptions()
		if err != nil {
			t.Fatal(err)
		}
		opts.Address = ant.APIAddr
		c := client.New(opts)
		if _, err := c.ConsensusGet(); err != nil {
			t.Fatal(err)
		}
	}
}

// TestTestConnectAnts verifies that ants will connect
func TestConnectAnts(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// connectAnts should throw an error if only one ant is provided
	if err := connectAnts(&ant.Ant{}); err == nil {
		t.Fatal("connectAnts didnt throw an error with only one ant")
	}

	// Create minimum configs
	dataDir := test.TestDir(t.Name())
	antDirs := []string{}
	for i := 0; i < 5; i++ {
		path := filepath.Join(dataDir, strconv.Itoa(i))
		antDirs = append(antDirs, path)

		// Clean ant dirs
		os.RemoveAll(path)
		os.MkdirAll(path, 0700)
	}
	configs := []ant.AntConfig{
		{
			SiadConfig: ant.SiadConfig{
				DataDir:  antDirs[0],
				SiadPath: test.TestSiadPath,
			},
		},
		{
			SiadConfig: ant.SiadConfig{
				DataDir:  antDirs[1],
				SiadPath: test.TestSiadPath,
			},
		},
		{
			SiadConfig: ant.SiadConfig{
				DataDir:  antDirs[2],
				SiadPath: test.TestSiadPath,
			},
		},
		{
			SiadConfig: ant.SiadConfig{
				DataDir:  antDirs[3],
				SiadPath: test.TestSiadPath,
			},
		},
		{
			SiadConfig: ant.SiadConfig{
				DataDir:  antDirs[4],
				SiadPath: test.TestSiadPath,
			},
		},
	}

	// Start ants
	ants, err := startAnts(configs...)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, ant := range ants {
			ant.Close()
		}
	}()

	// Connect the ants
	err = connectAnts(ants...)
	if err != nil {
		t.Fatal(err)
	}

	// Get the Gateway info from on of the ants
	opts, err := client.DefaultOptions()
	if err != nil {
		t.Fatal(err)
	}
	opts.Address = ants[0].APIAddr
	c := client.New(opts)
	gatewayInfo, err := c.GatewayGet()
	if err != nil {
		t.Fatal(err)
	}

	// Verify the ants are peers
	for _, ant := range ants[1:] {
		hasAddr := false
		for _, peer := range gatewayInfo.Peers {
			if fmt.Sprintf("%s", peer.NetAddress) == ant.RPCAddr {
				hasAddr = true
			}
		}
		if !hasAddr {
			t.Fatalf("the central ant is missing %v", ant.RPCAddr)
		}
	}
}

// TestTestAntConsensusGroups probes the antConsensusGroup function
func TestAntConsensusGroups(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create minimum configs
	dataDir := test.TestDir(t.Name())
	configs := []ant.AntConfig{
		{
			SiadConfig: ant.SiadConfig{
				DataDir:  dataDir,
				SiadPath: test.TestSiadPath,
			},
		},
		{
			SiadConfig: ant.SiadConfig{
				DataDir:  dataDir,
				SiadPath: test.TestSiadPath,
			},
		},
		{
			SiadConfig: ant.SiadConfig{
				DataDir:  dataDir,
				SiadPath: test.TestSiadPath,
			},
		},
	}

	// Start Ants
	ants, err := startAnts(configs...)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, ant := range ants {
			ant.Close()
		}
	}()

	// Get the consensus groups
	groups, err := antConsensusGroups(ants...)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Fatal("expected 1 consensus group initially")
	}
	if len(groups[0]) != len(ants) {
		t.Fatal("expected the consensus group to have all the ants")
	}

	// Start an ant that is desynced from the rest of the network
	cfg, err := parseConfig(ant.AntConfig{Jobs: []string{"miner"}})
	if err != nil {
		t.Fatal(err)
	}
	otherAnt, err := ant.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ants = append(ants, otherAnt)

	// Wait for the other ant to mine a few blocks
	time.Sleep(time.Second * 30)

	// Verify the ants are synced
	groups, err = antConsensusGroups(ants...)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatal("expected 2 consensus groups")
	}
	if len(groups[0]) != len(ants)-1 {
		t.Fatal("expected the first consensus group to have 3 ants")
	}
	if len(groups[1]) != 1 {
		t.Fatal("expected the second consensus group to have 1 ant")
	}
	if !reflect.DeepEqual(groups[1][0], otherAnt) {
		t.Fatal("expected the miner ant to be in the second consensus group")
	}
}
