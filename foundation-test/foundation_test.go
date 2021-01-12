// Package foundationtest implements Foundation hardfork tests.
package foundationtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/ant"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/antfarm"
	binariesbuilder "gitlab.com/NebulousLabs/Sia-Ant-Farm/binaries-builder"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/persist"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/test"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/upnprouter"
	"gitlab.com/NebulousLabs/Sia/build"
	"gitlab.com/NebulousLabs/Sia/crypto"
	"gitlab.com/NebulousLabs/Sia/modules"
	"gitlab.com/NebulousLabs/Sia/node/api/client"
	"gitlab.com/NebulousLabs/Sia/types"
	"gitlab.com/NebulousLabs/encoding"
	"gitlab.com/NebulousLabs/errors"
)

// Tests configs
const (
	// asicHardforkTimeout defines timeout for waiting for ASIC hardfork
	// blockheight.
	asicHardforkTimeout = time.Minute * 3

	// allowLocalIPs defines whether we allow ants to use localhost IPs.
	// Default is true. When set to true it is possible to test from Sia v1.5.0
	// on Gitlab CI and on machines without publicly accessible ports and
	// without UPnP enabled router. When set to false, currently it allows to
	// test with external IPs on network with UPnP enabled router.
	allowLocalIPs = true // xxx default false

	// binariesDir defines path where build binaries should be stored. If the
	// path is set as relative, it is relative to Sia-Ant-Farm/foundation-test
	// directory.
	binariesDir = "../upgrade-binaries"

	// forceBinaryRebuilding defines if binary should be rebuilt even though it
	// already exists. It saves time when repeating tests.
	forceBinaryRebuilding = true

	// foundationSiadFilename is the foundation siad file name in PATH used
	// for foundation testing
	foundationSiadFilename = "siad-foundation-dev" //xxx use builder

	// foundationSubsidyIntervalTimeout defines timeout for waiting between
	// Foundation subsidy payouts.
	foundationSubsidyIntervalTimeout = time.Minute * 5

	// hardforkMatureTimeout defines timeout for waiting for Foundation subsidy
	// hardfork + maturity delay blockheight. Make it long enough when the
	// tests run in parallel.
	hardforkMatureTimeout = time.Minute * 12

	// minerFundedTimeout defines timeout for miner to have enough Siacoins to
	// send to other ants.
	minerFundedTimeout = time.Minute * 2

	// nonHardforkSiaVersion defines Sia version, that has not implemented
	// Foundation hardfork.
	nonHardforkSiaVersion = "v1.5.3"

	// secondRegularSubsidyMatureTimeout defines timeout for waiting for the
	// second regular monthly Foundation subsidy + maturity delay blockheight.
	secondRegularSubsidyMatureTimeout = hardforkMatureTimeout + time.Minute*10

	// transactionConfirmationTimeout defines timeout for waiting for a
	// transaction to become confirmed.
	transactionConfirmationTimeout = time.Minute * 2
)

var (
	// Prepare test values
	hardforkMatureBH             = types.FoundationHardforkHeight + types.MaturityDelay
	firstRegularSubsidyMatureBH  = hardforkMatureBH + types.FoundationSubsidyFrequency
	secondRegularSubsidyMatureBH = firstRegularSubsidyMatureBH + types.FoundationSubsidyFrequency

	regularSubsidy = types.FoundationSubsidyPerBlock.Mul64(uint64(types.FoundationSubsidyFrequency))

	// Prepare expected error
	errNonExistingOutput = errors.New("transaction spends a nonexisting siacoin output")
)

// TestFoundationFailsafeAddressCanChangeUnlockHashes tests that the initial
// Foundation failsafe address can change primary and failsafe Foundation
// addesses.
func TestFoundationFailsafeAddressCanChangeUnlockHashes(t *testing.T) {
	if !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()

	// Check that the test runs with dev build tag
	if build.Release != "dev" {
		t.Fatal("this test is expected to be executed with dev build tag to load dev constants from Sia")
	}

	// Prepare logger
	dataDir := test.TestDir(t.Name())
	logger := test.NewTestLogger(t, dataDir)
	defer func() {
		if err := logger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Check UPnP enabled router
	upnpStatus := upnprouter.CheckUPnPEnabled()
	logger.Debugln(upnpStatus)

	// Config antfarm with a miner and 3 generic ants. 2 of them become new
	// Foundation primary and failsafe address ants.
	antfarmConfig, err := antfarm.NewAntfarmConfig(dataDir, allowLocalIPs, 1, 0, 0, 3)
	if err != nil {
		t.Fatal(err)
	}

	// Update config to use foundation siad dev binaries
	for i := range antfarmConfig.AntConfigs {
		antfarmConfig.AntConfigs[i].SiadPath = foundationSiadFilename
	}

	// Create antfarm
	farm, err := antfarm.New(logger, antfarmConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := farm.Close(); err != nil {
			logger.Errorf("can't close antfarm: %v", err)
		}
	}()

	// Create miner client
	m, err := farm.GetAntByName(ant.NameMiner(0))
	if err != nil {
		t.Fatal(err)
	}
	mc, err := m.NewClient()
	if err != nil {
		t.Fatal(err)
	}

	// Wait till miner has Siacoins
	value := types.SiacoinPrecision.Mul64(3)
	err = m.WaitConfirmedSiacoinBalance(ant.BalanceGreater, value, minerFundedTimeout)
	if err != nil {
		t.Fatal(err)
	}

	// Get the Foundation failsafe address and keys
	foundationFailsafeUnlockConditions, foundationFailsafeKeys := types.GenerateDeterministicMultisig(3, 5, types.InitialFoundationFailsafeTestingSalt)

	// Get current block height
	cg, err := mc.ConsensusGet()
	if err != nil {
		t.Fatal(err)
	}
	beforePostBH := cg.Height

	// Send Siacoins to the Foundation failsafe address
	_, err = mc.WalletSiacoinsPost(value, foundationFailsafeUnlockConditions.UnlockHash(), false)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for transaction to appear in the blockchain
	waitBH := types.BlockHeight(5)
	err = m.WaitForBlockHeight(beforePostBH+waitBH, time.Minute, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Get output ID
	var outputID types.SiacoinOutputID
	var found bool
outputIDFinder:
	for bh := types.BlockHeight(beforePostBH + 1); bh < types.BlockHeight(beforePostBH+waitBH); bh++ {
		cbg, err := mc.ConsensusBlocksHeightGet(bh)
		if err != nil {
			t.Fatal(err)
		}
		for _, tx := range cbg.Transactions {
			for _, sco := range tx.SiacoinOutputs {
				if sco.Value.Cmp(value) == 0 {
					outputID = sco.ID
					found = true
					break outputIDFinder
				}
			}
		}
	}
	if !found {
		t.Fatal("output of the post transaction was not found in the blockchain")
	}

	// Get generic ant client
	g1, err := farm.GetAntByName(ant.NameGeneric(0))
	if err != nil {
		t.Fatal(err)
	}
	c1, err := g1.NewClient()
	if err != nil {
		t.Fatal(err)
	}

	// Get generic ant ownning new primary address
	newPrimaryAnt, err := farm.GetAntByName(ant.NameGeneric(1))
	if err != nil {
		t.Fatal(err)
	}
	newPrimaryAddress, err := newPrimaryAnt.WalletAddress()
	if err != nil {
		t.Fatal(err)
	}

	// Get generic ant owning new failsafe address
	newFailsafeAnt, err := farm.GetAntByName(ant.NameGeneric(2))
	if err != nil {
		t.Fatal(err)
	}
	newFailsafeAddress, err := newFailsafeAnt.WalletAddress()
	if err != nil {
		t.Fatal(err)
	}

	// Wait for Foundation hardfork
	err = g1.WaitForBlockHeight(types.FoundationHardforkHeight, hardforkMatureTimeout, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Check initial Foundation unlock hashes
	err = checkFoundationUnlockHashes(mc, types.InitialFoundationUnlockHash, types.InitialFoundationFailsafeUnlockHash)
	if err != nil {
		t.Fatal(err)
	}

	// Change primary and failsafe unlock hashes
	err = changeFoundationUnlockHashes(c1, outputID, value, foundationFailsafeUnlockConditions, foundationFailsafeKeys, types.InitialFoundationFailsafeUnlockHash, *newPrimaryAddress, *newFailsafeAddress)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for initial Foundation subsidy
	err = newPrimaryAnt.WaitForBlockHeight(hardforkMatureBH, foundationSubsidyIntervalTimeout, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Check updated Foundation unlock hashes
	err = checkFoundationUnlockHashes(mc, *newPrimaryAddress, *newFailsafeAddress)
	if err != nil {
		t.Fatal(err)
	}

	// Check new primary ant received regular subsidy
	err = newPrimaryAnt.WaitConfirmedSiacoinBalance(ant.BalanceEquals, regularSubsidy, foundationSubsidyIntervalTimeout)
	if err != nil {
		t.Fatal(err)
	}
}

// TestFoundationPrimaryAddressCanChangeUnlockHashes tests that the initial
// Foundation primary address can change primary and failsafe Foundation
// addesses.
func TestFoundationPrimaryAddressCanChangeUnlockHashes(t *testing.T) {
	if !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()

	// Check that the test runs with dev build tag
	if build.Release != "dev" {
		t.Fatal("this test is expected to be executed with dev build tag to load dev constants from Sia")
	}

	// Prepare logger
	dataDir := test.TestDir(t.Name())
	logger := test.NewTestLogger(t, dataDir)
	defer func() {
		if err := logger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Check UPnP enabled router
	upnpStatus := upnprouter.CheckUPnPEnabled()
	logger.Debugln(upnpStatus)

	// Config antfarm with a miner and 2 generic ants which become new
	// Foundation primary and failsafe address ants.
	antfarmConfig, err := antfarm.NewAntfarmConfig(dataDir, allowLocalIPs, 1, 0, 0, 3)
	if err != nil {
		t.Fatal(err)
	}

	// Update config to use foundation siad dev binaries
	for i := range antfarmConfig.AntConfigs {
		antfarmConfig.AntConfigs[i].SiadPath = foundationSiadFilename
	}

	// Create antfarm
	farm, err := antfarm.New(logger, antfarmConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := farm.Close(); err != nil {
			logger.Errorf("can't close antfarm: %v", err)
		}
	}()

	// Get generic ant client
	g1, err := farm.GetAntByName(ant.NameGeneric(0))
	if err != nil {
		t.Fatal(err)
	}
	c1, err := g1.NewClient()
	if err != nil {
		t.Fatal(err)
	}

	// Wait for initial Foundation subsidy
	err = g1.WaitForBlockHeight(hardforkMatureBH, hardforkMatureTimeout, time.Second)
	if err != nil {
		t.Fatalf("Foundation hardfork + maturity delay blockheight not reached: %v", err)
	}

	// Get subsidy output ID
	cbhg, err := c1.ConsensusBlocksHeightGet(types.FoundationHardforkHeight)
	if err != nil {
		t.Fatal(err)
	}
	subsidyID := cbhg.ID.FoundationSubsidyID()

	// Get the Foundation primary address and keys
	foundationPrimaryUnlockConditions, foundationPrimaryKeys := types.GenerateDeterministicMultisig(2, 3, types.InitialFoundationTestingSalt)

	// Get generic ant ownning new primary address
	newPrimaryAnt, err := farm.GetAntByName(ant.NameGeneric(1))
	if err != nil {
		t.Fatal(err)
	}
	newPrimaryAddress, err := newPrimaryAnt.WalletAddress()
	if err != nil {
		t.Fatal(err)
	}

	// Get generic ant owning new failsafe address
	newFailsafeAnt, err := farm.GetAntByName(ant.NameGeneric(2))
	if err != nil {
		t.Fatal(err)
	}
	newFailsafeAddress, err := newFailsafeAnt.WalletAddress()
	if err != nil {
		t.Fatal(err)
	}

	// Check initial Foundation unlock hashes
	err = checkFoundationUnlockHashes(c1, types.InitialFoundationUnlockHash, types.InitialFoundationFailsafeUnlockHash)
	if err != nil {
		t.Fatal(err)
	}

	// Change primary and failsafe unlock hashes
	err = changeFoundationUnlockHashes(c1, subsidyID, types.InitialFoundationSubsidy, foundationPrimaryUnlockConditions, foundationPrimaryKeys, types.InitialFoundationUnlockHash, *newPrimaryAddress, *newFailsafeAddress)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for next subsidy
	err = g1.WaitForBlockHeight(firstRegularSubsidyMatureBH, time.Minute*2, time.Second)
	if err != nil {
		t.Fatalf("Waiting for next subsidy failed: %v", err)
	}

	// Check updated Foundation unlock hashes
	err = checkFoundationUnlockHashes(c1, *newPrimaryAddress, *newFailsafeAddress)
	if err != nil {
		t.Fatal(err)
	}

	// Check new primary ant received subsidy
	err = newPrimaryAnt.WaitConfirmedSiacoinBalance(ant.BalanceEquals, regularSubsidy, time.Minute*2)
	if err != nil {
		t.Fatal(err)
	}
}

// TestFoundationPrimaryAddressCanSendSiacoins tests that the wallet owning
// initial Foundation primary address can send Siacoins to another address.
func TestFoundationPrimaryAddressCanSendSiacoins(t *testing.T) {
	if !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()

	// Check that the test runs with dev build tag
	if build.Release != "dev" {
		t.Fatal("this test is expected to be executed with dev build tag to load dev constants from Sia")
	}

	// Prepare logger
	dataDir := test.TestDir(t.Name())
	logger := test.NewTestLogger(t, dataDir)
	defer func() {
		if err := logger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Check UPnP enabled router
	upnpStatus := upnprouter.CheckUPnPEnabled()
	logger.Debugln(upnpStatus)

	// Config antfarm with a miner and 2 generic ants
	antfarmConfig, err := antfarm.NewAntfarmConfig(dataDir, allowLocalIPs, 1, 0, 0, 2)
	if err != nil {
		t.Fatal(err)
	}

	// Update config to use foundation siad dev binaries
	for i := range antfarmConfig.AntConfigs {
		antfarmConfig.AntConfigs[i].SiadPath = foundationSiadFilename
	}

	// Create antfarm
	farm, err := antfarm.New(logger, antfarmConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := farm.Close(); err != nil {
			logger.Errorf("can't close antfarm: %v", err)
		}
	}()

	// Get generic ant client
	g1, err := farm.GetAntByName(ant.NameGeneric(0))
	if err != nil {
		t.Fatal(err)
	}
	c1, err := g1.NewClient()
	if err != nil {
		t.Fatal(err)
	}

	// Wait for initial Foundation subsidy
	err = g1.WaitForBlockHeight(hardforkMatureBH, hardforkMatureTimeout, time.Second)
	if err != nil {
		t.Fatalf("Foundation hardfork + maturity delay blockheight not reached: %v", err)
	}

	// Get receiving ant's client and address
	receivingAnt, err := farm.GetAntByName(ant.NameGeneric(1))
	if err != nil {
		t.Fatal(err)
	}
	receivingAddress, err := receivingAnt.WalletAddress()
	if err != nil {
		t.Fatal(err)
	}

	// Get the Foundation primary address and keys.
	foundationPrimaryUnlockConditions, foundationPrimaryKeys := types.GenerateDeterministicMultisig(2, 3, types.InitialFoundationTestingSalt)

	// Get Foundation hardfork subsidyID
	cbhg, err := c1.ConsensusBlocksHeightGet(types.FoundationHardforkHeight)
	if err != nil {
		t.Fatal(err)
	}
	subsidyID := cbhg.ID.FoundationSubsidyID()

	// Send Siacoins from Foundation primary address to receiving ant
	amount := types.InitialFoundationSubsidy.Sub(types.SiacoinPrecision)
	minerFee := types.SiacoinPrecision
	err = sendSiacoinsFromFoundationPrimaryAddress(c1, subsidyID, foundationPrimaryUnlockConditions, foundationPrimaryKeys, amount, minerFee, *receivingAddress)
	if err != nil {
		t.Fatal(err)
	}

	// Check receiving ant received Siacoins
	err = receivingAnt.WaitConfirmedSiacoinBalance(ant.BalanceEquals, amount, time.Minute*2)
	if err != nil {
		t.Fatal(err)
	}
}

// TestFoundationPrimaryAddressReceivesSubsidies tests that the wallet owning
// initial Foundation primary address receives exactly initial Foundation
// subsidy and 2 regular monthly subsidies.
func TestFoundationPrimaryAddressReceivesSubsidies(t *testing.T) {
	if !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()

	// Check that the test runs with dev build tag
	if build.Release != "dev" {
		t.Fatal("this test is expected to be executed with dev build tag to load dev constants from Sia")
	}

	// Prepare logger
	dataDir := test.TestDir(t.Name())
	logger := test.NewTestLogger(t, dataDir)
	defer func() {
		if err := logger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Check UPnP enabled router
	upnpStatus := upnprouter.CheckUPnPEnabled()
	logger.Debugln(upnpStatus)

	// Config antfarm with a miner and 2 generic ants
	antfarmConfig, err := antfarm.NewAntfarmConfig(dataDir, allowLocalIPs, 1, 0, 0, 2)
	if err != nil {
		t.Fatal(err)
	}

	// Update config to use foundation siad dev binaries
	for i := range antfarmConfig.AntConfigs {
		antfarmConfig.AntConfigs[i].SiadPath = foundationSiadFilename
	}

	// Create antfarm
	farm, err := antfarm.New(logger, antfarmConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := farm.Close(); err != nil {
			logger.Errorf("can't close antfarm: %v", err)
		}
	}()

	// Get generic ant client
	g1, err := farm.GetAntByName(ant.NameGeneric(0))
	if err != nil {
		t.Fatal(err)
	}
	c1, err := g1.NewClient()
	if err != nil {
		t.Fatal(err)
	}

	// Get generic ant 2 client and address
	g2, err := farm.GetAntByName(ant.NameGeneric(1))
	if err != nil {
		t.Fatal(err)
	}
	a, err := g2.WalletAddress()
	if err != nil {
		t.Fatal(err)
	}
	address := *a

	// Get the foundation primary address and keys.
	foundationPrimaryUnlockConditions, foundationPrimaryKeys := types.GenerateDeterministicMultisig(2, 3, types.InitialFoundationTestingSalt)

	// Check initial Foundation subsidy and 2 more months
	start := time.Now()
	var subsidyID types.SiacoinOutputID
	var lastCheckedBH types.BlockHeight
	var sentInitialSubsidy, sentFirstRegularSubsidy, sentSecondRegularSubsidy bool
	for {
		// Get block height
		cg, err := c1.ConsensusGet()
		if err != nil {
			t.Fatal(err)
		}
		bh := cg.Height

		// Timeout Foundation hardfork
		if time.Since(start) > hardforkMatureTimeout && bh < hardforkMatureBH {
			t.Fatalf("Foundation hardfork + maturity delay blockheight not reached within %v timeout. Current block height: %v, expected block height: %v", hardforkMatureTimeout, bh, hardforkMatureBH)
		}

		// Timeout Foundation hardfork + 2 more regular subsidies
		if time.Since(start) > secondRegularSubsidyMatureTimeout {
			t.Fatalf("second regular monthly subsidy + maturity delay blockheight not reached within %v timeout. Current block height: %v, expected block height: %v", secondRegularSubsidyMatureTimeout, bh, secondRegularSubsidyMatureBH)
		}

		if bh == lastCheckedBH {
			// Nothing new, wait
			time.Sleep(time.Millisecond * 200)
			continue
		}

		if bh >= types.MaturityDelay {
			// Get foundation subsidyID
			cbhg, err := c1.ConsensusBlocksHeightGet(bh - types.MaturityDelay)
			if err != nil {
				t.Fatal(err)
			}
			subsidyID = cbhg.ID.FoundationSubsidyID()
		}

		switch {
		// Before foundation hardfork and maturity delay
		case bh >= types.MaturityDelay && bh < types.FoundationHardforkHeight+types.MaturityDelay:
			// Verify foundation primary address has no Siacoins by trying to
			// send out a hasting
			err := sendSiacoinsFromFoundationPrimaryAddress(c1, subsidyID, foundationPrimaryUnlockConditions, foundationPrimaryKeys, types.NewCurrency64(1), types.NewCurrency64(1), address)
			// errors.Contains() doesn't work and misses an error, need to
			// compare strings
			if !strings.Contains(err.Error(), errNonExistingOutput.Error()) {
				t.Fatal("Foundation primary address contains unexpected Siacons before foundation hardfork and maturity delay")
			}
		// After foundation hardfork and maturity delay, but before first
		// regular monthly subsidy and maturity delay
		case bh >= hardforkMatureBH && bh < firstRegularSubsidyMatureBH:
			// Check the foundation primary address has initial subsidy by
			// sending it to another address
			if !sentInitialSubsidy {
				// Fix subsidyID if we have skipped the exact hardfork mature
				// block
				if bh != hardforkMatureBH {
					cbhg, err := c1.ConsensusBlocksHeightGet(types.FoundationHardforkHeight)
					if err != nil {
						t.Fatal(err)
					}
					subsidyID = cbhg.ID.FoundationSubsidyID()
				}

				// Send Siacoins
				err = sendSiacoinsFromFoundationPrimaryAddress(c1, subsidyID, foundationPrimaryUnlockConditions, foundationPrimaryKeys, types.InitialFoundationSubsidy.Sub(types.SiacoinPrecision), types.SiacoinPrecision, address)
				if err != nil {
					t.Fatal("Foundation primary address doesn't contain expected Siacons after foundation hardfork and maturity delay")
				}
				sentInitialSubsidy = true
			}
			// Check there are no more Siacoins
			err := sendSiacoinsFromFoundationPrimaryAddress(c1, subsidyID, foundationPrimaryUnlockConditions, foundationPrimaryKeys, types.NewCurrency64(1), types.NewCurrency64(1), address)
			// errors.Contains() doesn't work and misses an error, need to
			// compare strings
			if !strings.Contains(err.Error(), errNonExistingOutput.Error()) {
				t.Fatal("Foundation primary address contains unexpected Siacons after foundation hardfork and maturity delay")
			}

		// After first regular monthly subsidy before the second one
		case bh >= firstRegularSubsidyMatureBH && bh < secondRegularSubsidyMatureBH:
			// Check the foundation primary address has the first regular
			// subsidy by sending it to another address
			if !sentFirstRegularSubsidy {
				// Fix subsidyID if we have skipped the exact first regular
				// subsidy mature block
				if bh != firstRegularSubsidyMatureBH {
					cbhg, err := c1.ConsensusBlocksHeightGet(types.FoundationHardforkHeight + types.FoundationSubsidyFrequency)
					if err != nil {
						t.Fatal(err)
					}
					subsidyID = cbhg.ID.FoundationSubsidyID()
				}

				// Send Siacoins
				err := sendSiacoinsFromFoundationPrimaryAddress(c1, subsidyID, foundationPrimaryUnlockConditions, foundationPrimaryKeys, regularSubsidy.Sub(types.SiacoinPrecision), types.SiacoinPrecision, address)
				if err != nil {
					t.Fatal("Foundation primary address doesn't contain expected Siacons after first regular subsidy and maturity delay")
				}
				sentFirstRegularSubsidy = true
			}
			// Check there are no more Siacoins
			err := sendSiacoinsFromFoundationPrimaryAddress(c1, subsidyID, foundationPrimaryUnlockConditions, foundationPrimaryKeys, types.NewCurrency64(1), types.NewCurrency64(1), address)
			// errors.Contains() doesn't work and misses an error, need to
			// compare strings
			if !strings.Contains(err.Error(), errNonExistingOutput.Error()) {
				t.Fatal("Foundation primary address contains unexpected Siacons after first regular subsidy and maturity delay")
			}

		// After second regular monthly subsidy
		case bh >= secondRegularSubsidyMatureBH:
			// Check the foundation primary address has the second regular
			// subsidy by sending it to another address
			if !sentSecondRegularSubsidy {
				// Fix subsidyID if we have skipped the exact second regular
				// subsidy mature block
				if bh != secondRegularSubsidyMatureBH {
					cbhg, err := c1.ConsensusBlocksHeightGet(types.FoundationHardforkHeight + 2*types.FoundationSubsidyFrequency)
					if err != nil {
						t.Fatal(err)
					}
					subsidyID = cbhg.ID.FoundationSubsidyID()
				}

				// Send Siacoins
				err := sendSiacoinsFromFoundationPrimaryAddress(c1, subsidyID, foundationPrimaryUnlockConditions, foundationPrimaryKeys, regularSubsidy.Sub(types.SiacoinPrecision), types.SiacoinPrecision, address)
				if err != nil {
					t.Fatal("Foundation primary address doesn't contain expected Siacons after second regular subsidy and maturity delay")
				}
				sentSecondRegularSubsidy = true
			}
			// Check there are no more Siacoins
			err := sendSiacoinsFromFoundationPrimaryAddress(c1, subsidyID, foundationPrimaryUnlockConditions, foundationPrimaryKeys, types.NewCurrency64(1), types.NewCurrency64(1), address)
			// errors.Contains() doesn't work and misses an error, need to
			// compare strings
			if !strings.Contains(err.Error(), errNonExistingOutput.Error()) {
				t.Fatal("Foundation primary address contains unexpected Siacons after second regular subsidy and maturity delay")
			}

			// We are done
			return
		}
		lastCheckedBH = bh
		time.Sleep(time.Millisecond * 200)
	}
}

// TestTransactionWithWrongReplayProtectionByteIsRejected tests that
// transactions executed on the main (Foundation) blockchain can't be replied
// on the legacy (non-Foundation) blockchain.
func TestTransactionWithWrongReplayProtectionByteIsRejected(t *testing.T) {
	if !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()

	// Check that the test runs with dev build tag
	if build.Release != "dev" {
		t.Fatal("this test is expected to be executed with dev build tag to load dev constants from Sia")
	}

	// Prepare logger
	dataDir := test.TestDir(t.Name())
	logger := test.NewTestLogger(t, dataDir)
	defer func() {
		if err := logger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Check UPnP enabled router
	upnpStatus := upnprouter.CheckUPnPEnabled()
	logger.Debugln(upnpStatus)

	// Build the pre-hardfork binary
	if _, err := os.Stat(binariesbuilder.SiadBinaryPath(nonHardforkSiaVersion)); err != nil || forceBinaryRebuilding {
		err = binariesbuilder.StaticBuilder.BuildVersions(logger, nonHardforkSiaVersion)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Create antfarm config with non-hardfork siad binaries
	antfarmDataDir := filepath.Join(dataDir, "antfarm-data")
	legacyBlockchainAntfarmDataDir := antfarmDataDir + "-legacy-blockchain"

	// legacyBlockchainAntDirs must be set before antfarm data is copied,
	// otherwise the data is deleted by test.AntDirs
	legacyBlockchainAntDirs, err := test.AntDirs(legacyBlockchainAntfarmDataDir, 3)
	if err != nil {
		t.Fatalf("can't create legacy blockchain ant data directories: %v", err)
	}
	nonHardforkSiadPath := binariesbuilder.SiadBinaryPath(nonHardforkSiaVersion)

	// Config antfarm with a miner and 2 generic ants
	antfarmConfig, err := antfarm.NewAntfarmConfig(antfarmDataDir, allowLocalIPs, 1, 0, 0, 2)
	if err != nil {
		t.Fatal(err)
	}

	// Update config to use foundation siad dev binaries //xxx not right
	for i := range antfarmConfig.AntConfigs {
		antfarmConfig.AntConfigs[i].SiadPath = nonHardforkSiadPath
	}

	// Create antfarm
	farm, err := antfarm.New(logger, antfarmConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := farm.Close(); err != nil {
			logger.Errorf("can't close antfarm: %v", err)
		}
	}()

	// Get miner and client
	m, err := farm.GetAntByName(ant.NameMiner(0))
	if err != nil {
		t.Fatal(err)
	}
	mc, err := m.NewClient()
	if err != nil {
		t.Fatal(err)
	}

	// Wait till miner has Siacoins
	value1 := types.SiacoinPrecision.Mul64(3)
	value2 := types.SiacoinPrecision.Mul64(4)
	err = m.WaitConfirmedSiacoinBalance(ant.BalanceGreater, value1.Add(value2), minerFundedTimeout)
	if err != nil {
		t.Fatal(err)
	}

	// Get generic ant 1, client and address
	g1, err := farm.GetAntByName(ant.NameGeneric(0))
	if err != nil {
		t.Fatal(err)
	}
	g1Address, err := g1.WalletAddress()
	if err != nil {
		t.Fatal(err)
	}
	c1, err := g1.NewClient()
	if err != nil {
		t.Fatal(err)
	}

	// Send Siacoins for pre-hardfork replay transaction
	_, err = mc.WalletSiacoinsPost(value1, *g1Address, false)
	if err != nil {
		t.Fatal(err)
	}

	// Send Siacoins for post-hardfork replay transaction
	_, err = mc.WalletSiacoinsPost(value2, *g1Address, false)
	if err != nil {
		t.Fatal(err)
	}

	// Wait till an1 receives Siacoins
	err = g1.WaitConfirmedSiacoinBalance(ant.BalanceEquals, value1.Add(value2), transactionConfirmationTimeout)
	if err != nil {
		t.Fatal(err)
	}

	// Stop ants before saving ant's data directories and Foundation upgrade
	for _, a := range farm.Ants {
		err := a.Close()
		if err != nil {
			t.Fatal(err)
		}
	}

	// Copy ants' data for legacy (non-hardfork) blockchain
	cmd := binariesbuilder.Command{
		Name: "cp",
		Args: []string{"-r", antfarmDataDir + "/.", legacyBlockchainAntfarmDataDir},
	}
	out, err := cmd.Execute(logger)
	if err != nil {
		t.Fatalf("can't copy antfarm datadir: %v\n%v", err, out)
	}

	// Update ants to use Foundation siad binary. Update them concurrently so
	// that we have them up and running before hardfork.
	errChan := make(chan error, len(farm.Ants))
	for _, a := range farm.Ants {
		go func(logger *persist.Logger, a *ant.Ant, errChan chan error) {
			err := a.StartSiad(foundationSiadFilename)
			errChan <- err
		}(logger, a, errChan)
	}
	for range farm.Ants {
		err := <-errChan
		if err != nil {
			t.Fatal(err)
		}
	}
	close(errChan)

	// Wait for ASIC hardfork height so that we can replay the first transacion
	// after ASIC hardfork, before Foundation hardfork.
	err = g1.WaitForBlockHeight(types.ASICHardforkHeight, asicHardforkTimeout, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Get prepared wallet unspent outputs
	wug, err := c1.WalletUnspentGet()
	if err != nil {
		t.Fatal(err)
	}
	var output1, output2 modules.UnspentOutput
	for _, uo := range wug.Outputs {
		switch {
		case uo.Value.Equals(value1):
			output1 = uo
		case uo.Value.Equals(value2):
			output2 = uo
		}
	}
	if !output1.Value.Equals(value1) || !output2.Value.Equals(value2) {
		t.Fatal("didn't found the expected outputs")
	}

	// Prepare unlock conditions
	wucg1, err := c1.WalletUnlockConditionsGet(output1.UnlockHash)
	if err != nil {
		t.Fatal(err)
	}

	// Get generic ant 2, client and address
	g2, err := farm.GetAntByName(ant.NameGeneric(1))
	if err != nil {
		t.Fatal(err)
	}
	c2, err := g2.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	g2Address, err := g2.WalletAddress()
	if err != nil {
		t.Fatal(err)
	}

	// Create transaction for replay before hardfork
	minerFee := types.SiacoinPrecision
	txn1, err := createSendSiacoinsTransaction(c1, types.SiacoinOutputID(output1.ID), wucg1.UnlockConditions, output1.Value.Sub(minerFee), minerFee, *g2Address)
	if err != nil {
		t.Fatal(err)
	}

	// Post transaction sending Siacoins to ant 2
	err = c1.TransactionPoolRawPost(txn1, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for ant 2 to receive Siacoins before hardfork
	start := time.Now()
	for {
		// Timeout
		if time.Since(start) > transactionConfirmationTimeout {
			t.Fatalf("waiting for transaction to become confirmed reached %v timeout", transactionConfirmationTimeout)
		}

		wg, err := c2.WalletGet()
		if err != nil {
			t.Fatal(err)
		}

		// Hardfork blockheight check
		if wg.Height > types.FoundationHardforkHeight {
			t.Fatal("waiting for transaction to become confirmed reached Foundation hardfork height")
		}

		// Done
		if wg.ConfirmedSiacoinBalance.Cmp(value1.Sub(minerFee)) >= 0 {
			break
		}

		time.Sleep(time.Second)
	}

	// Wait for Foundation hardfork
	err = g1.WaitForBlockHeight(hardforkMatureBH, hardforkMatureTimeout, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Prepare unlock conditions 2
	wucg2, err := c1.WalletUnlockConditionsGet(output2.UnlockHash)
	if err != nil {
		t.Fatal(err)
	}

	// Create transaction 2 for replay after hardfork
	txn2, err := createSendSiacoinsTransaction(c1, types.SiacoinOutputID(output2.ID), wucg2.UnlockConditions, output2.Value.Sub(minerFee), minerFee, *g2Address)
	if err != nil {
		t.Fatal(err)
	}

	// Post transaction 2 sending Siacoins to ant 2
	err = c1.TransactionPoolRawPost(txn2, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for ant2 to receive Siacoins after hardfork
	err = g2.WaitConfirmedSiacoinBalance(ant.BalanceEquals, value1.Add(value2).Sub(minerFee.Mul64(2)), transactionConfirmationTimeout)
	if err != nil {
		t.Fatal(err)
	}

	// Stop ants before testing on legacy (non-hardfork) blockchain
	for _, a := range farm.Ants {
		err := a.Close()
		if err != nil {
			t.Fatal(err)
		}
	}

	// Update ants to use non-Foundation siad binary. Update them concurrently
	// so that we have them up and running before the hardfork blockheight.
	errChan = make(chan error, len(farm.Ants))
	for i, a := range farm.Ants {
		// Change ants data directory for legacy blockchain
		a.Config.SiadConfig.DataDir = legacyBlockchainAntDirs[i]

		// Wake up ants on legacy blockchain
		go func(logger *persist.Logger, a *ant.Ant, errChan chan error) {
			err := a.StartSiad(nonHardforkSiadPath)
			errChan <- err
		}(logger, a, errChan)
	}
	for range farm.Ants {
		err := <-errChan
		if err != nil {
			t.Fatal(err)
		}
	}
	close(errChan)

	// Wait for ASIC hardfork height so that we can replay the first transacion
	err = g1.WaitForBlockHeight(types.ASICHardforkHeight, asicHardforkTimeout, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Reply transaction 1 sending Siacoins to ant 2 before hardfork height
	err = c1.TransactionPoolRawPost(txn1, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for ant 2 to receive Siacoins from reply transaction 1 before
	// hardfork height on legacy blockchain
	start = time.Now()
	for {
		// Timeout
		if time.Since(start) > transactionConfirmationTimeout {
			t.Fatalf("waiting for transaction to become confirmed reached %v timeout", transactionConfirmationTimeout)
		}

		wg, err := c2.WalletGet()
		if err != nil {
			t.Fatal(err)
		}

		// Hardfork blockheight check
		if wg.Height > types.FoundationHardforkHeight {
			t.Fatal("waiting for transaction to become confirmed reached Foundation hardfork height")
		}

		// Done
		if wg.ConfirmedSiacoinBalance.Cmp(value1.Sub(minerFee)) == 0 {
			break
		}

		time.Sleep(time.Second)
	}

	// Wait for Foundation hardfork + maturity delay height on legacy
	// blockchain.
	err = g1.WaitForBlockHeight(hardforkMatureBH, hardforkMatureTimeout, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Try to reply transaction sending Siacoins to ant2 after hardfork height.
	// The replay should fail because of Foundation hardfork replay protection.
	err = c1.TransactionPoolRawPost(txn2, nil)
	// errors.Contains() doesn't work here, so we check error strings.
	if err == nil || !strings.Contains(err.Error(), crypto.ErrInvalidSignature.Error()) {
		t.Fatal(err)
	}
}

// changeFoundationUnlockHashes creates and posts the transaction to change
// Foundation primary and Foundation failsafe unlock hashes.
func changeFoundationUnlockHashes(c *client.Client, siacoinInputParentID types.SiacoinOutputID, outputValue types.Currency, siacoinInputUnlockConditions types.UnlockConditions, keys []crypto.SecretKey, outputUH, newPrimaryUH, newFailsafeUH types.UnlockHash) error {
	// Get current block height
	cg, err := c.ConsensusGet()
	if err != nil {
		return errors.AddContext(err, "can't get consensus")
	}
	currentHeight := cg.Height

	// Create a transaction
	txn := types.Transaction{
		SiacoinInputs: []types.SiacoinInput{{
			ParentID:         siacoinInputParentID,
			UnlockConditions: siacoinInputUnlockConditions,
		}},
		SiacoinOutputs: []types.SiacoinOutput{
			{
				Value:      outputValue,
				UnlockHash: outputUH,
			},
		},
		ArbitraryData: [][]byte{encoding.MarshalAll(types.SpecifierFoundation, types.FoundationUnlockHashUpdate{
			NewPrimary:  newPrimaryUH,
			NewFailsafe: newFailsafeUH,
		})},
		TransactionSignatures: make([]types.TransactionSignature, siacoinInputUnlockConditions.SignaturesRequired),
	}

	// Sign the transaction
	for i := range txn.TransactionSignatures {
		txn.TransactionSignatures[i].ParentID = crypto.Hash(siacoinInputParentID)
		txn.TransactionSignatures[i].CoveredFields = types.FullCoveredFields
		txn.TransactionSignatures[i].PublicKeyIndex = uint64(i)
		sig := crypto.SignHash(txn.SigHash(i, currentHeight), keys[i])
		txn.TransactionSignatures[i].Signature = sig[:]
	}

	// Check transaction valid
	err = txn.StandaloneValid(currentHeight)
	if err != nil {
		return errors.AddContext(err, "transaction is not valid")
	}

	// Post the transaction
	err = c.TransactionPoolRawPost(txn, nil)
	if err != nil {
		return errors.AddContext(err, "error posting transaction")
	}
	return nil
}
