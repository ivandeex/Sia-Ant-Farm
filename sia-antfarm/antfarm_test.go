package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/ant"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/test"
	"gitlab.com/NebulousLabs/Sia/node/api/client"
)

// verify that createAntfarm() creates a new antfarm correctly.
func TestNewAntfarm(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	antFarmAddr := test.RandomLocalhostAddress()
	antAddr := test.RandomLocalhostAddress()
	dataDir := test.TestDir(t.Name())
	antDirs := initAntDirs(dataDir, 1)
	config := AntfarmConfig{
		ListenAddress: antFarmAddr,
		AntConfigs: []ant.AntConfig{
			{
				SiadConfig: ant.SiadConfig{
					DataDir:  antDirs[0],
					RPCAddr:  antAddr,
					SiadPath: test.TestSiadPath,
				},
				Jobs: []string{
					"gateway",
				},
			},
		},
	}

	antfarm, err := createAntfarm(config)
	if err != nil {
		t.Fatal(err)
	}
	defer antfarm.Close()

	go antfarm.ServeAPI()

	res, err := http.DefaultClient.Get("http://" + antFarmAddr + "/ants")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	var ants []*ant.Ant
	err = json.NewDecoder(res.Body).Decode(&ants)
	if err != nil {
		t.Fatal(err)
	}
	if len(ants) != len(config.AntConfigs) {
		t.Fatal("expected /ants to return the correct number of ants")
	}
	if ants[0].RPCAddr != config.AntConfigs[0].RPCAddr {
		t.Fatal("expected /ants to return the correct rpc address")
	}
}

// verify that connectExternalAntfarm connects antfarms to eachother correctly
func TestConnectExternalAntfarm(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	datadir := test.TestDir(t.Name())
	antConfig := ant.AntConfig{
		SiadConfig: ant.SiadConfig{
			DataDir:  datadir,
			RPCAddr:  "127.0.0.1:3338",
			SiadPath: test.TestSiadPath,
		},
		Jobs: []string{
			"gateway",
		},
	}

	config1 := AntfarmConfig{
		ListenAddress: "127.0.0.1:31337",
		DataDirPrefix: "antfarm-data1",
		AntConfigs:    []ant.AntConfig{antConfig},
	}

	config2 := AntfarmConfig{
		ListenAddress: "127.0.0.1:31338",
		DataDirPrefix: "antfarm-data2",
		AntConfigs:    []ant.AntConfig{antConfig},
	}

	farm1, err := createAntfarm(config1)
	if err != nil {
		t.Fatal(err)
	}
	defer farm1.Close()
	go farm1.ServeAPI()

	farm2, err := createAntfarm(config2)
	if err != nil {
		t.Fatal(err)
	}
	defer farm2.Close()
	go farm2.ServeAPI()

	err = farm1.connectExternalAntfarm(config2.ListenAddress)
	if err != nil {
		t.Fatal(err)
	}

	// give a bit of time for the connection to succeed
	time.Sleep(time.Second * 3)

	// verify that farm2 has farm1 as its peer
	opts, err := client.DefaultOptions()
	if err != nil {
		t.Fatal(err)
	}
	opts.Address = farm1.ants[0].APIAddr
	c := client.New(opts)
	gatewayInfo, err := c.GatewayGet()
	if err != nil {
		t.Fatal(err)
	}

	for _, ant := range farm2.ants {
		hasAddr := false
		for _, peer := range gatewayInfo.Peers {
			if fmt.Sprintln(peer.NetAddress) == ant.RPCAddr {
				hasAddr = true
			}
		}
		if !hasAddr {
			t.Fatalf("farm1 is missing %v", ant.RPCAddr)
		}
	}
}
