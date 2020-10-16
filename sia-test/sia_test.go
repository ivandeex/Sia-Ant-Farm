package siatest

import (
	"path/filepath"
	"testing"
	"time"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/antfarm"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/test"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/utils"
	"gitlab.com/NebulousLabs/Sia/build"
	"gitlab.com/NebulousLabs/Sia/modules"
	"gitlab.com/NebulousLabs/Sia/types"
)

const (
	// allowLocalIPs defines whether we allow ants to use localhost IPs.
	// Default is true. When set to true it is possible to test from Sia v1.5.0
	// on Gitlab CI and on machines without publicly accessible ports and
	// without UPnP enabled router. When set to false, currently it allows to
	// test with external IPs on network with UPnP enabled router.
	allowLocalIPs = true //xxx
)

//xxx
func TestInsufficientMaxDuration(t *testing.T) {
	if !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()

	// xxx upnp

	// Get default Antfarm config
	dataDir := test.TestDir(t.Name())
	antfarmConfig := antfarm.NewDefaultRenterAntfarmTestingConfig(dataDir, allowLocalIPs)

	// Set renter job without default allowance
	renterIndex, err := antfarmConfig.GetAntConfigIndexByName(test.RenterAntName)
	if err != nil {
		t.Fatal(err)
	}
	antfarmConfig.AntConfigs[renterIndex].Jobs = []string{"noAllowanceRenter"}

	// Start antfarm
	farm, err := antfarm.New(antfarmConfig)
	if err != nil {
		t.Fatal(err)
	}

	// Set custom renter allowance
	period := 40
	allowance := modules.Allowance{
		Funds:       types.NewCurrency64(20e3).Mul(types.SiacoinPrecision),
		Hosts:       5,
		Period:      types.BlockHeight(period),
		RenewWindow: types.BlockHeight(period / 4),

		ExpectedStorage:    10e9,
		ExpectedUpload:     uint64(2e9 / period),
		ExpectedDownload:   uint64(1e12 / period),
		ExpectedRedundancy: 3.0,
		MaxPeriodChurn:     2.5e9,
	}
	renterAnt, err := farm.GetAntByName(test.RenterAntName)
	err = renterAnt.Jr.StaticClient.RenterPostAllowance(allowance)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for renter upload ready
	renterAnt.Jr.WaitForRenterUploadReady()

	// Check the max duration message is not yet found
	insufficientMaxDurationMsg := "contract renewal with host was unsuccessful; insufficient MaxDuration of host"
	contractorLog := filepath.Join(renterAnt.Config.DataDir, "renter/contractor.log")
	found, err := utils.FileContains(contractorLog, insufficientMaxDurationMsg)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("didn't expect to find an insufficient MaxDuration message yet")
	}

	// Get latest contract end
	rc, err := renterAnt.Jr.StaticClient.RenterContractsGet()
	if err != nil {
		t.Fatal(err)
	}
	var latestContractEnd types.BlockHeight
	for _, ac := range rc.ActiveContracts {
		ace := ac.EndHeight
		if ace > latestContractEnd {
			latestContractEnd = ace
		}
	}

	// Lower a max duration on one host
	//xxx

	// Wait till contracts renew
	timeout := time.Minute * 4
	start := time.Now()
	for {
		// Timeout
		if time.Since(start) > timeout {
			t.Fatalf("latest contract end height was not reached within %v timeout", timeout)
		}
		cg, err := renterAnt.Jr.StaticClient.ConsensusGet()
		if err != nil {
			t.Fatal(err)
		}
		if cg.Height > latestContractEnd {
			break
		}
		time.Sleep(time.Second)
	}

	found, err = utils.FileContains(contractorLog, insufficientMaxDurationMsg)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected to find an insufficient MaxDuration message")
	}
}
