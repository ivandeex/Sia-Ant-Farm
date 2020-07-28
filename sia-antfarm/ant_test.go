package main

import (
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"gitlab.com/NebulousLabs/errors"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/ant"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/test"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/upnprouter"
	"gitlab.com/NebulousLabs/Sia/modules"
	"gitlab.com/NebulousLabs/Sia/node/api/client"
	"gitlab.com/NebulousLabs/Sia/types"
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
	antDirs := test.AntDirs(dataDir, 3)
	configs := []ant.AntConfig{
		{
			SiadConfig: ant.SiadConfig{
				AllowHostLocalNetAddress: true,
				DataDir:                  antDirs[0],
				SiadPath:                 test.TestSiadPath,
			},
		},
		{
			SiadConfig: ant.SiadConfig{
				AllowHostLocalNetAddress: true,
				DataDir:                  antDirs[1],
				SiadPath:                 test.TestSiadPath,
			},
		},
		{
			SiadConfig: ant.SiadConfig{
				AllowHostLocalNetAddress: true,
				DataDir:                  antDirs[2],
				SiadPath:                 test.TestSiadPath,
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

// TestRenterDisableIPViolationCheck verifies that IPViolationCheck can be set
// via renter ant config
func TestRenterDisableIPViolationCheck(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Define test cases data
	testCases := []struct {
		name                          string
		dataDirPostfix                string
		renterDisableIPViolationCheck bool
	}{
		{"TestDefaultIPViolationCheck", "-default", false},
		{"TestDisabledIPViolationCheck", "-ip-check-disabled", true},
	}

	// Run test cases
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create minimum configs
			dataDir := test.TestDir(t.Name() + tc.dataDirPostfix)
			antDirs := test.AntDirs(dataDir, 1)
			configs := []ant.AntConfig{
				{
					SiadConfig: ant.SiadConfig{
						AllowHostLocalNetAddress: true,
						DataDir:                  antDirs[0],
						SiadPath:                 test.TestSiadPath,
					},
					Jobs: []string{"renter"},
				},
			}

			// Update config if testing disabled IP violation check
			if tc.renterDisableIPViolationCheck {
				configs[0].RenterDisableIPViolationCheck = true
			}

			// Start ant
			ants, err := startAnts(configs...)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				for _, ant := range ants {
					ant.Close()
				}
			}()
			renterAnt := ants[0]

			// Get http client
			c, err := getClient(renterAnt.APIAddr, "")
			if err != nil {
				t.Fatal(err)
			}

			// Get renter settings
			renterInfo, err := c.RenterGet()
			if err != nil {
				t.Fatal(err)
			}
			// Check that IP violation check was not set by default and was set
			// correctly if configured so
			if !tc.renterDisableIPViolationCheck && !renterInfo.Settings.IPViolationCheck {
				t.Fatal("Setting IPViolationCheck is supposed to be true by default")
			} else if tc.renterDisableIPViolationCheck && renterInfo.Settings.IPViolationCheck {
				t.Fatal("Setting IPViolationCheck is supposed to be set false by the ant config")
			}
		})
	}
}

// TestConnectAnts verifies that ants will connect
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
	antDirs := test.AntDirs(dataDir, 5)
	configs := []ant.AntConfig{
		{
			SiadConfig: ant.SiadConfig{
				AllowHostLocalNetAddress: true,
				DataDir:                  antDirs[0],
				SiadPath:                 test.TestSiadPath,
			},
		},
		{
			SiadConfig: ant.SiadConfig{
				AllowHostLocalNetAddress: true,
				DataDir:                  antDirs[1],
				SiadPath:                 test.TestSiadPath,
			},
		},
		{
			SiadConfig: ant.SiadConfig{
				AllowHostLocalNetAddress: true,
				DataDir:                  antDirs[2],
				SiadPath:                 test.TestSiadPath,
			},
		},
		{
			SiadConfig: ant.SiadConfig{
				AllowHostLocalNetAddress: true,
				DataDir:                  antDirs[3],
				SiadPath:                 test.TestSiadPath,
			},
		},
		{
			SiadConfig: ant.SiadConfig{
				AllowHostLocalNetAddress: true,
				DataDir:                  antDirs[4],
				SiadPath:                 test.TestSiadPath,
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

	// Get the Gateway info from one of the ants
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
			if fmt.Sprint(peer.NetAddress) == ant.RPCAddr {
				hasAddr = true
				break
			}
		}
		if !hasAddr {
			t.Fatalf("the central ant is missing %v", ant.RPCAddr)
		}
	}
}

// TestAntConsensusGroups probes the antConsensusGroup function
func TestAntConsensusGroups(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create minimum configs
	dataDir := test.TestDir(t.Name())
	antDirs := test.AntDirs(dataDir, 4)
	configs := []ant.AntConfig{
		{
			SiadConfig: ant.SiadConfig{
				AllowHostLocalNetAddress: true,
				DataDir:                  antDirs[0],
				SiadPath:                 test.TestSiadPath,
			},
		},
		{
			SiadConfig: ant.SiadConfig{
				AllowHostLocalNetAddress: true,
				DataDir:                  antDirs[1],
				SiadPath:                 test.TestSiadPath,
			},
		},
		{
			SiadConfig: ant.SiadConfig{
				AllowHostLocalNetAddress: true,
				DataDir:                  antDirs[2],
				SiadPath:                 test.TestSiadPath,
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
	cfg, err := parseConfig(ant.AntConfig{
		Jobs: []string{"miner"},
		SiadConfig: ant.SiadConfig{
			AllowHostLocalNetAddress: true,
			DataDir:                  antDirs[3],
			SiadPath:                 test.TestSiadPath,
		},
	},
	)
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

// TestVerifyUploadDownloadFileData uploads and downloads a file and checks
// that their content is identical by comparing their merkle root hashes
func TestVerifyUploadDownloadFileData(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create minimum configs
	dataDir := test.TestDir(t.Name())
	antDirs := test.AntDirs(dataDir, 7)
	configs := []ant.AntConfig{
		{
			SiadConfig: ant.SiadConfig{
				AllowHostLocalNetAddress: true,
				DataDir:                  antDirs[0],
				SiadPath:                 test.TestSiadPath,
			},
			Jobs: []string{"gateway", "miner"},
		},
		{
			SiadConfig: ant.SiadConfig{
				AllowHostLocalNetAddress: true,
				DataDir:                  antDirs[1],
				SiadPath:                 test.TestSiadPath,
			},
			Jobs:            []string{"host"},
			DesiredCurrency: 100000,
		},
		{
			SiadConfig: ant.SiadConfig{
				AllowHostLocalNetAddress: true,
				DataDir:                  antDirs[2],
				SiadPath:                 test.TestSiadPath,
			},
			Jobs:            []string{"host"},
			DesiredCurrency: 100000,
		},
		{
			SiadConfig: ant.SiadConfig{
				AllowHostLocalNetAddress: true,
				DataDir:                  antDirs[3],
				SiadPath:                 test.TestSiadPath,
			},
			Jobs:            []string{"host"},
			DesiredCurrency: 100000,
		},
		{
			SiadConfig: ant.SiadConfig{
				AllowHostLocalNetAddress: true,
				DataDir:                  antDirs[4],
				SiadPath:                 test.TestSiadPath,
			},
			Jobs:            []string{"host"},
			DesiredCurrency: 100000,
		},
		{
			SiadConfig: ant.SiadConfig{
				AllowHostLocalNetAddress: true,
				DataDir:                  antDirs[5],
				SiadPath:                 test.TestSiadPath,
			},
			Jobs:            []string{"host"},
			DesiredCurrency: 100000,
		},
		{
			SiadConfig: ant.SiadConfig{
				AllowHostLocalNetAddress: true,
				DataDir:                  antDirs[6],
				SiadPath:                 test.TestSiadPath,
			},
			DesiredCurrency: 100000,
		},
	}

	// Check whether UPnP is enabled on router to speed up if testing without
	// UPnP enabled router
	upnprouter.CheckUPnPEnabled()

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

	// Wait for ants to sync
	ant.AntSyncWG.Add(1)
	err = waitForAntsToSync(antsSyncTimeout, ants...)
	if err != nil {
		t.Fatal(errors.AddContext(err, "wait for ants to sync failed"))
	}

	// Wait for renter wallet to be filled
	renterAnt := ants[6]

	desiredCurrency := types.NewCurrency64(renterAnt.Config.DesiredCurrency).Mul(types.SiacoinPrecision)
	checkFrequency := ant.BalanceCheckFrequency
	balanceTimeout := ant.InitialBalanceWarningTimeout
	renterAnt.Jr.BlockUntilWalletIsFilled("renter", desiredCurrency, checkFrequency, balanceTimeout)

	// Set allowance
	r := ant.RenterJob{
		StaticJR: renterAnt.Jr,
	}
	err = r.SetAllowance(ant.DefaultAntfarmAllowance, ant.SetAllowanceFrequency, ant.SetAllowanceWarningTimeout)
	if err != nil {
		t.Fatal(errors.AddContext(err, "couldn't set renter allowance"))
	}

	// Disable IP violation check
	err = r.DisableIPViolationCheck()
	if err != nil {
		t.Fatal("Couldn't disable IP violation check")
	}

	// Wait for renter upload ready
	err = r.WaitForUploadReady()
	if err != nil {
		t.Fatalf("Renter didn't become upload ready; %v\n", err)
	}

	// Prepare upload directory
	err = r.CreateSourceFilesDir()
	if err != nil {
		t.Fatal(err)
	}

	// Check no files are uploaded yet
	if len(r.Files) > 0 {
		t.Fatal("There are already some unexpected uploaded files")
	}

	// Upload the file
	err = r.ManagedUpload(modules.SectorSize)
	if err != nil {
		t.Fatal(err)
	}

	// Check exactly 1 file is uploaded
	if l := len(r.Files); l != 1 {
		t.Fatalf("Uploaded files: Expected: 1, got %d\n", l)
	}

	// Download a file
	f := r.Files[0].SourceFile
	sp, err := modules.NewSiaPath(f)
	if err != nil {
		t.Fatalf("Can't create SiaPath from %v. Got error: %v", f, err)
	}
	fi := modules.FileInfo{SiaPath: sp}
	file, err := r.ManagedDownload(fi)
	if err != nil {
		t.Fatalf("Can't download a file. Error: %v", err)
	}

	// Verify the file content hash
	// Need to reopen the file
	file, err = os.Open(file.Name())
	if err != nil {
		t.Fatalf("Can't open downloaded file. Error: %v", err)
	}
	defer file.Close()
	root, err := ant.MerkleRoot(file)
	if err != nil {
		t.Fatalf("Can't get merkle root. Error: %v", err)
	}
	if root != r.Files[0].MerkleRoot {
		t.Fatal("Downloaded file merkle root doesn't equal uploaded file merkle root")
	}
}
