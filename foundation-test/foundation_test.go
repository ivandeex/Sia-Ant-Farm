// Package foundationtest implements Foundation hardfork tests.
package foundationtest

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/ant"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/antfarm"
	binariesbuilder "gitlab.com/NebulousLabs/Sia-Ant-Farm/binaries-builder"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/test"
	"gitlab.com/NebulousLabs/Sia/crypto"
	"gitlab.com/NebulousLabs/Sia/modules"
	"gitlab.com/NebulousLabs/Sia/types"
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
	allowLocalIPs = false

	// forcePreHardforkBinaryRebuilding defines if pre-hardfork binary should
	// be rebuilt even though it already exists. It saves time when repeating
	// tests.
	forcePreHardforkBinaryRebuilding = true

	// forceFoundationBinaryRebuilding defines if Foundation binary should be
	// rebuilt even though it already exists. It saves time when repeating
	// tests.
	forceFoundationBinaryRebuilding = true

	// foundationSiaVersion defines Sia version, that has implemented
	// Foundation hardfork.
	foundationSiaVersion = "v1.5.4"

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
	// Init Foundation test.
	logger, dataDir := initFoundationTest(t)
	defer func() {
		if err := logger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Config antfarm with a miner and 3 generic ants. 2 of them become new
	// Foundation primary and failsafe address ants.
	farm, err := initDefaultFoundationAntfarm(logger, dataDir, 3)
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
	mc := m.StaticClient

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
	c1 := g1.StaticClient

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
	// Init Foundation test.
	logger, dataDir := initFoundationTest(t)
	defer func() {
		if err := logger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Config antfarm with a miner and 3 generic ants. 2 of them become new
	// Foundation primary and failsafe address ants.
	farm, err := initDefaultFoundationAntfarm(logger, dataDir, 3)
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
	c1 := g1.StaticClient

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
	// Init Foundation test.
	logger, dataDir := initFoundationTest(t)
	defer func() {
		if err := logger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	//  Config antfarm with a miner and 2 generic ants.
	farm, err := initDefaultFoundationAntfarm(logger, dataDir, 2)
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
	c1 := g1.StaticClient

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
	// Init Foundation test.
	logger, dataDir := initFoundationTest(t)
	defer func() {
		if err := logger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	//  Config antfarm with a miner and 2 generic ants.
	farm, err := initDefaultFoundationAntfarm(logger, dataDir, 2)
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
	c1 := g1.StaticClient

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
	var bh, lastLogBH types.BlockHeight
	var subsidyID types.SiacoinOutputID
	sendInitialSubsidy, sendRegularSubsidy := true, true
	value1 := types.InitialFoundationSubsidy.Sub(types.SiacoinPrecision)
	value2 := regularSubsidy.Sub(types.SiacoinPrecision)
	totalValue := value1.Add(value2.Mul64(2))
blockHeightLoop:
	for {
		// Get block height
		cg, err := c1.ConsensusGet()
		if err != nil {
			t.Fatal(err)
		}
		if cg.Height == bh {
			// Nothing new, wait
			time.Sleep(time.Millisecond * 200)
			continue
		}
		bh = cg.Height

		// Log progress
		if bh >= lastLogBH+5 {
			logger.Debugf("Current block height: %v", bh)
			lastLogBH = bh
		}

		// Timeout Foundation hardfork
		if time.Since(start) > hardforkMatureTimeout && bh < hardforkMatureBH {
			t.Fatalf("Foundation hardfork + maturity delay blockheight not reached within %v timeout. Current block height: %v, expected block height: %v", hardforkMatureTimeout, bh, hardforkMatureBH)
		}

		// Timeout Foundation hardfork + 2 more regular subsidies
		if time.Since(start) > secondRegularSubsidyMatureTimeout {
			t.Fatalf("second regular monthly subsidy + maturity delay blockheight not reached within %v timeout. Current block height: %v, expected block height: %v", secondRegularSubsidyMatureTimeout, bh, secondRegularSubsidyMatureBH)
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
			err := forwardFoundationSubsidy(logger, c1, false, bh, types.BlockHeight(0), subsidyID, foundationPrimaryUnlockConditions, foundationPrimaryKeys, types.SiacoinPrecision, address)
			if err != nil {
				t.Fatal(err)
			}

		// After foundation hardfork and maturity delay, but before first
		// regular monthly subsidy and maturity delay
		case bh >= hardforkMatureBH && bh < firstRegularSubsidyMatureBH:
			// Check the foundation primary address has initial subsidy by
			// sending it to another address. Try sending it twice, but it
			// should be sent and received just once.
			err := forwardFoundationSubsidyTwiceCheckReceivedOnce(logger, c1, sendInitialSubsidy, bh, types.FoundationHardforkHeight, subsidyID, foundationPrimaryUnlockConditions, foundationPrimaryKeys, value1, value1, address, g2)
			if err != nil {
				t.Fatal(err)
			}
			sendInitialSubsidy = false
		// After first regular monthly subsidy before the second one
		case bh >= firstRegularSubsidyMatureBH && bh < secondRegularSubsidyMatureBH:
			// Check the foundation primary address has the first regular
			// subsidy by sending it to another address. Try sending it twice,
			// but it should be sent and received just once.
			err := forwardFoundationSubsidyTwiceCheckReceivedOnce(logger, c1, sendRegularSubsidy, bh, types.FoundationHardforkHeight+types.FoundationSubsidyFrequency, subsidyID, foundationPrimaryUnlockConditions, foundationPrimaryKeys, value2, value1.Add(value2), address, g2)
			if err != nil {
				t.Fatal(err)
			}
			sendRegularSubsidy = false
		// After second regular monthly subsidy
		case bh >= secondRegularSubsidyMatureBH:
			// Check the foundation primary address has the second regular
			// subsidy by sending it to another address. Try sending it twice,
			// but it should be sent and received just once.
			err := forwardFoundationSubsidyTwiceCheckReceivedOnce(logger, c1, true, bh, types.FoundationHardforkHeight+2*types.FoundationSubsidyFrequency, subsidyID, foundationPrimaryUnlockConditions, foundationPrimaryKeys, value2, totalValue, address, g2)
			if err != nil {
				t.Fatal(err)
			}
			break blockHeightLoop
		}
	}

	// Check final value of receiving ant wallet after a couple of blocks
	waitBH := types.BlockHeight(20)
	err = g2.WaitForBlockHeight(bh+waitBH, transactionConfirmationTimeout*2, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	err = g2.WaitConfirmedSiacoinBalance(ant.BalanceEquals, totalValue, time.Second)
	if err != nil {
		t.Fatal(err)
	}
}

// TestFoundationUploadsDownloads tests that uploading and downloading works
// before Foundation hardfork, after the hardfork and after 2 renewal periods.
// In each interval it uploads files and downloads all files i.e. uploaded in
// the current and in the previous intervals.
func TestFoundationUploadsDownloads(t *testing.T) {
	// Init Foundation test.
	logger, dataDir := initFoundationTest(t)
	defer func() {
		if err := logger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Build the pre-hardfork binary
	nonHardforkSiadPath := binariesbuilder.SiadBinaryPath(nonHardforkSiaVersion)
	err := binariesbuilder.StaticBuilder.BuildVersions(logger, forcePreHardforkBinaryRebuilding, nonHardforkSiaVersion)
	if err != nil {
		t.Fatal(err)
	}

	// Build the Foundation binary
	foundationSiadPath := binariesbuilder.SiadBinaryPath(foundationSiaVersion)
	err = binariesbuilder.StaticBuilder.BuildVersions(logger, forceFoundationBinaryRebuilding, foundationSiaVersion)
	if err != nil {
		t.Fatal(err)
	}

	// Config antfarm with a miner, 5 hosts, a renter and a generic ant
	hosts := 5
	antfarmConfig, err := antfarm.NewAntfarmConfig(dataDir, allowLocalIPs, 1, hosts, 1, 1)
	if err != nil {
		t.Fatal(err)
	}

	// Update config to use non-hardfork siad dev binaries
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

	// Get renter ant
	r, err := farm.GetAntByName(ant.NameRenter(0))
	if err != nil {
		t.Fatal(err)
	}

	// Timeout the test if the renter doesn't become upload ready
	err = r.Jr.WaitForRenterUploadReady()
	if err != nil {
		t.Error(err)
	}

	// Prepare upload and download
	fileSize := uint64(modules.SectorSize * 16)
	filesCount := 5
	renterJob := r.Jr.NewRenterJob()
	uploadedFiles := []ant.RenterFile{}

	uploadDownload := func() error {
		// Upload files
		for i := 0; i < filesCount; i++ {
			_, err = renterJob.Upload(fileSize)
			if err != nil {
				return errors.AddContext(err, "can't upload files")
			}
		}

		// Update uploaded files. We keep track of the uploaded files in a
		// separate variable, because renterJob.Files are reset after a new
		// renterJob is created after hardfork.
		uploadedFiles = append(uploadedFiles, renterJob.Files[len(renterJob.Files)-filesCount:]...)

		// Download files
		err = antfarm.DownloadAndVerifyFiles(logger, r, uploadedFiles)
		if err != nil {
			return errors.AddContext(err, "can't download files")
		}
		return nil
	}

	// Upload, download before Foundation hardfork
	err = uploadDownload()
	if err != nil {
		t.Fatal(err)
	}

	// Check we didn't crossed Foundation hardfork before upgrade
	rc := r.StaticClient
	cg, err := rc.ConsensusGet()
	if err != nil {
		t.Fatal(err)
	}
	if cg.Height >= types.FoundationHardforkHeight {
		t.Fatalf("current block height %v is higher than Foundation hardfork height %v, the test is invalid", cg.Height, types.FoundationHardforkHeight)
	}

	// Stop ants before upgrade
	for _, a := range farm.Ants {
		err := a.Close()
		if err != nil {
			t.Fatal(err)
		}
	}

	// Upgrade ants before hardfork
	err = updateAnts(farm, nil, foundationSiadPath)
	if err != nil {
		t.Fatal(err)
	}

	// Check we didn't crossed Foundation hardfork after upgrade
	cg, err = rc.ConsensusGet()
	if err != nil {
		t.Fatal(err)
	}
	if cg.Height >= types.FoundationHardforkHeight {
		t.Fatalf("current block height %v is higher than Foundation hardfork height %v, the test is invalid", cg.Height, types.FoundationHardforkHeight)
	}

	err = r.WaitForBlockHeight(types.FoundationHardforkHeight, hardforkMatureTimeout, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Get new renterJob after upgrade for upload and download
	renterJob = r.Jr.NewRenterJob()

	// Upload, download after Foundation hardfork
	err = uploadDownload()
	if err != nil {
		t.Fatal(err)
	}

	// Get current block height
	cg, err = rc.ConsensusGet()
	if err != nil {
		t.Fatal(err)
	}

	// Get receiving ant's client and address
	receivingAnt, err := farm.GetAntByName(ant.NameGeneric(0))
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
	cbhg, err := rc.ConsensusBlocksHeightGet(types.FoundationHardforkHeight)
	if err != nil {
		t.Fatal(err)
	}
	subsidyID := cbhg.ID.FoundationSubsidyID()

	// Wait for Foundation hardfork mature so we can send out Siacoins from the
	// Foundation primary address
	err = r.WaitForBlockHeight(hardforkMatureBH, transactionConfirmationTimeout, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Send Siacoins from Foundation primary address to receiving ant
	amount := types.InitialFoundationSubsidy.Sub(types.SiacoinPrecision)
	minerFee := types.SiacoinPrecision
	err = sendSiacoinsFromFoundationPrimaryAddress(rc, subsidyID, foundationPrimaryUnlockConditions, foundationPrimaryKeys, amount, minerFee, *receivingAddress)
	if err != nil {
		t.Fatal(err)
	}

	// Check receiving ant received Siacoins
	err = receivingAnt.WaitConfirmedSiacoinBalance(ant.BalanceEquals, amount, time.Minute*2)
	if err != nil {
		t.Fatal(err)
	}

	// Get renter allowance period
	rg, err := rc.RenterGet()
	if err != nil {
		t.Fatal(err)
	}
	allowancePeriod := rg.Settings.Allowance.Period

	// Wait for a contract renewal
	timeout := time.Minute * 10
	err = r.WaitForBlockHeight(cg.Height+allowancePeriod, timeout, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Upload, download after the first renewal period
	err = uploadDownload()
	if err != nil {
		t.Fatal(err)
	}

	// Wait for the second contract renewal
	err = r.WaitForBlockHeight(cg.Height+allowancePeriod*2, timeout, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Upload, download after the second renewal period
	err = uploadDownload()
	if err != nil {
		t.Fatal(err)
	}
}

// TestReplayProtection tests that transactions executed on the legacy (non-
// Foundation-hardfork) blockchain can't be replayed on Foundation hardfork
// blockchain.
func TestReplayProtection(t *testing.T) {
	// Init Foundation test.
	logger, dataDir := initFoundationTest(t)
	defer func() {
		if err := logger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Build the pre-hardfork binary
	nonHardforkSiadPath := binariesbuilder.SiadBinaryPath(nonHardforkSiaVersion)
	err := binariesbuilder.StaticBuilder.BuildVersions(logger, forcePreHardforkBinaryRebuilding, nonHardforkSiaVersion)
	if err != nil {
		t.Fatal(err)
	}

	// Build the Foundation binary
	foundationSiadPath := binariesbuilder.SiadBinaryPath(foundationSiaVersion)
	err = binariesbuilder.StaticBuilder.BuildVersions(logger, forceFoundationBinaryRebuilding, foundationSiaVersion)
	if err != nil {
		t.Fatal(err)
	}

	// Set antfarm data dirs
	antfarmDataDir := filepath.Join(dataDir, "antfarm-data-preparation")
	legacyBlockChainAntfarmDataDir := filepath.Join(dataDir, "antfarm-data-legacy")
	hardforkBlockChainAntfarmDataDir := filepath.Join(dataDir, "antfarm-data-hardfork")

	// Set ants' data dirs
	antDirs, err := test.AntDirs(antfarmDataDir, 3)
	if err != nil {
		t.Fatal(err)
	}
	legacyBlockChainAntDirs, err := test.AntDirs(legacyBlockChainAntfarmDataDir, 3)
	if err != nil {
		t.Fatal(err)
	}
	hardforkBlockChainAntDirs, err := test.AntDirs(hardforkBlockChainAntfarmDataDir, 3)
	if err != nil {
		t.Fatal(err)
	}

	// Config antfarm with a miner and 2 generic ants
	antfarmConfig, err := antfarm.NewAntfarmConfig(antfarmDataDir, allowLocalIPs, 1, 0, 0, 2)
	if err != nil {
		t.Fatal(err)
	}

	// Update config to use preparation ant dirs and non-hardfork siad dev binaries
	for i := range antfarmConfig.AntConfigs {
		antfarmConfig.AntConfigs[i].DataDir = antDirs[i]
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
	mc := m.StaticClient

	// Wait till miner has Siacoins
	value1 := types.SiacoinPrecision.Mul64(3)
	value2 := types.SiacoinPrecision.Mul64(4)
	totalValue := value1.Add(value2)
	err = m.WaitConfirmedSiacoinBalance(ant.BalanceGreater, totalValue, minerFundedTimeout)
	if err != nil {
		t.Fatal(err)
	}

	// Get generic ant1, client and address
	g1, err := farm.GetAntByName(ant.NameGeneric(0))
	if err != nil {
		t.Fatal(err)
	}
	g1Address, err := g1.WalletAddress()
	if err != nil {
		t.Fatal(err)
	}
	c1 := g1.StaticClient

	// Create spendable outputs
	_, err = mc.WalletSiacoinsPost(value1, *g1Address, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mc.WalletSiacoinsPost(value2, *g1Address, false)
	if err != nil {
		t.Fatal(err)
	}

	// Wait till ant1 receives Siacoins
	err = g1.WaitConfirmedSiacoinBalance(ant.BalanceEquals, totalValue, transactionConfirmationTimeout)
	if err != nil {
		t.Fatal(err)
	}

	// Stop ants before saving ant's data directories
	for _, a := range farm.Ants {
		err := a.Close()
		if err != nil {
			t.Fatal(err)
		}
	}

	// Copy ants' data for legacy (non-hardfork) blockchain
	cmd := binariesbuilder.Command{
		Name: "cp",
		Args: []string{"-r", antfarmDataDir + "/.", legacyBlockChainAntfarmDataDir},
	}
	out, err := cmd.Execute(logger)
	if err != nil {
		t.Fatalf("can't copy antfarm datadir: %v\n%v", err, out)
	}

	// Copy ants' data for Foundation hardfork blockchain
	cmd = binariesbuilder.Command{
		Name: "cp",
		Args: []string{"-r", antfarmDataDir + "/.", hardforkBlockChainAntfarmDataDir},
	}
	out, err = cmd.Execute(logger)
	if err != nil {
		t.Fatalf("can't copy antfarm datadir: %v\n%v", err, out)
	}

	// Update ants to use legacy data dirs and start them concurrently.
	err = updateAnts(farm, legacyBlockChainAntDirs, nonHardforkSiadPath)
	if err != nil {
		t.Fatal(err)
	}

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

	// Prepare unlock conditions 1
	wucg1, err := c1.WalletUnlockConditionsGet(output1.UnlockHash)
	if err != nil {
		t.Fatal(err)
	}

	// Get generic ant2, client and address
	g2, err := farm.GetAntByName(ant.NameGeneric(1))
	if err != nil {
		t.Fatal(err)
	}
	c2 := g2.StaticClient
	g2Address, err := g2.WalletAddress()
	if err != nil {
		t.Fatal(err)
	}

	// Create transaction 1 for replay before hardfork
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
	err = checkConfirmedBalanceBeforeBlockHeight(c2, types.FoundationHardforkHeight, transactionConfirmationTimeout, value1.Sub(minerFee))
	if err != nil {
		t.Fatal(err)
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

	// Stop ants before testing replay on Foundation hardfork blockchain
	for _, a := range farm.Ants {
		err := a.Close()
		if err != nil {
			t.Fatal(err)
		}
	}

	// Update ants to use Foundation binary with Foundation data dirs before
	// hardfork and start them concurrently.
	err = updateAnts(farm, hardforkBlockChainAntDirs, foundationSiadPath)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for ASIC hardfork height so that we can replay the first transacion
	err = g1.WaitForBlockHeight(types.ASICHardforkHeight, asicHardforkTimeout, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Replay transaction 1 sending Siacoins to ant 2 before hardfork height
	err = c1.TransactionPoolRawPost(txn1, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for ant 2 to receive Siacoins from replay of transaction 1 from
	// legacy blockchain before hardfork height on Foundation blockchain.
	err = checkConfirmedBalanceBeforeBlockHeight(c2, types.FoundationHardforkHeight, transactionConfirmationTimeout, value1.Sub(minerFee))
	if err != nil {
		t.Fatal(err)
	}

	// Wait for Foundation hardfork + maturity delay height on hardfork
	// blockchain.
	err = g1.WaitForBlockHeight(hardforkMatureBH, hardforkMatureTimeout, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Try to replay transaction 2 from legacy blockchain sending Siacoins to
	// ant2 after hardfork height. The replay should fail because of Foundation
	// hardfork replay protection.
	err = c1.TransactionPoolRawPost(txn2, nil)
	// errors.Contains() doesn't work here, so we check error strings.
	if err == nil || !strings.Contains(err.Error(), crypto.ErrInvalidSignature.Error()) {
		t.Fatal(err)
	}

	// Ant 2 should not receive Siacoins from replay of transaction 2 from
	// legacy blockchain after hardfork height on Foundation blockchain.
	err = g2.WaitConfirmedSiacoinBalance(ant.BalanceEquals, value1.Add(value2).Sub(minerFee.Mul64(2)), transactionConfirmationTimeout)
	if err == nil {
		t.Fatal(err)
	}
}
