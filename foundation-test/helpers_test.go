package foundationtest

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"go.sia.tech/sia-antfarm/ant"
	"go.sia.tech/sia-antfarm/antfarm"
	binariesbuilder "go.sia.tech/sia-antfarm/binaries-builder"
	"go.sia.tech/sia-antfarm/persist"
	"go.sia.tech/sia-antfarm/test"
	"go.sia.tech/sia-antfarm/upnprouter"
	"go.sia.tech/siad/build"
	"go.sia.tech/siad/crypto"
	"go.sia.tech/siad/node/api/client"
	"go.sia.tech/siad/types"
	"gitlab.com/NebulousLabs/encoding"
	"gitlab.com/NebulousLabs/errors"
)

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

// checkConfirmedBalanceBeforeBlockHeight checks that wallet balance equals
// given value before given timeout and block height are reached.
func checkConfirmedBalanceBeforeBlockHeight(c *client.Client, bh types.BlockHeight, timeout time.Duration, value types.Currency) error {
	start := time.Now()
	for {
		// Timeout
		if time.Since(start) > timeout {
			return fmt.Errorf("waiting for transaction to become confirmed reached %v timeout", transactionConfirmationTimeout)
		}

		wg, err := c.WalletGet()
		if err != nil {
			return err
		}

		// Hardfork blockheight check
		if wg.Height > bh {
			return fmt.Errorf("waiting for transaction to become confirmed reached block height %v", bh)
		}

		// Done
		if wg.ConfirmedSiacoinBalance.Cmp(value) == 0 {
			return nil
		}

		time.Sleep(time.Second)
	}
}

// checkFoundationUnlockHashes checks if expected Foundation unlock hashes
// match actual unlock hashes from consensus.
func checkFoundationUnlockHashes(c *client.Client, expectedPrimaryUnlockHash, expectedFailsafeUnlockHash types.UnlockHash) error {
	cg, err := c.ConsensusGet()
	if err != nil {
		return errors.AddContext(err, "can't get consensus")
	}
	var msg1 string
	if expectedPrimaryUnlockHash != cg.FoundationPrimaryUnlockHash {
		msg1 = fmt.Sprintf("Foundation primary unlock hashes do not match. Expected: %v, actual from consensus: %v", expectedPrimaryUnlockHash, cg.FoundationPrimaryUnlockHash)
	}
	var msg2 string
	if expectedFailsafeUnlockHash != cg.FoundationFailsafeUnlockHash {
		msg2 = fmt.Sprintf("Foundation failsafe unlock hashes do not match. Expected: %v, actual from consensus: %v", expectedFailsafeUnlockHash, cg.FoundationFailsafeUnlockHash)
	}
	if len(msg1) > 0 && len(msg2) > 0 {
		// Return both messages
		return errors.New(msg1 + "\n" + msg2)
	} else if len(msg1) > 0 || len(msg2) > 0 {
		// Return one message
		return errors.New(msg1 + msg2)
	}
	return nil
}

// createSendSiacoinsTransaction creates a transaction to sends Siacoins
// from an ant to the given address.
func createSendSiacoinsTransaction(c *client.Client, siacoinOutputID types.SiacoinOutputID, unlockConditions types.UnlockConditions, amount, minerFee types.Currency, address types.UnlockHash) (types.Transaction, error) {
	// Create a transaction
	txn := types.Transaction{
		SiacoinInputs: []types.SiacoinInput{{
			ParentID:         siacoinOutputID,
			UnlockConditions: unlockConditions,
		}},
		SiacoinOutputs: []types.SiacoinOutput{
			{
				Value:      amount,
				UnlockHash: address,
			},
		},
		MinerFees: []types.Currency{minerFee},
		TransactionSignatures: []types.TransactionSignature{
			{
				ParentID:       crypto.Hash(siacoinOutputID),
				PublicKeyIndex: 0,
				CoveredFields: types.CoveredFields{
					WholeTransaction: true,
				},
			},
		},
	}

	// Sign the transaction
	wspr, err := c.WalletSignPost(txn, []crypto.Hash{txn.TransactionSignatures[0].ParentID})
	if err != nil {
		return types.Transaction{}, errors.AddContext(err, "can't sign the transaction")
	}
	signedTxn := wspr.Transaction

	// Check transaction is valid. We create transactions on binaries without
	// Foundation hardfork and verify by calling Sia code with the Foundation
	// hardfork implemented, so we can make validation just before the hardfork
	// block height.
	cg, err := c.ConsensusGet()
	if err != nil {
		return types.Transaction{}, err
	}
	bh := cg.Height
	if bh < types.FoundationHardforkHeight {
		err = signedTxn.StandaloneValid(bh)
		if err != nil {
			return types.Transaction{}, errors.AddContext(err, "transaction is not valid")
		}
	}

	return signedTxn, nil
}

// forwardFoundationSubsidy sends the Foundation primary address received
// subsidy to another address if sendSubsidy is true. Then it tries to send out
// just one hasting which is expected to fail.
func forwardFoundationSubsidy(logger *persist.Logger, c *client.Client, sendSubsidy bool, currentBH, subsidyBH types.BlockHeight, subsidyID types.SiacoinOutputID, foundationPrimaryUnlockConditions types.UnlockConditions, foundationPrimaryKeys []crypto.SecretKey, value types.Currency, address types.UnlockHash) error {
	// Send expected subsidy
	if sendSubsidy {
		// Fix subsidyID if we have skipped the exact subsidy mature block
		if currentBH != subsidyBH+types.MaturityDelay {
			cbhg, err := c.ConsensusBlocksHeightGet(subsidyBH)
			if err != nil {
				return errors.AddContext(err, "can't get consensus blocks")
			}
			subsidyID = cbhg.ID.FoundationSubsidyID()
		}

		// Send Siacoins
		logger.Debugf("sending %v from Foundation primary address at block height %v", value, currentBH)
		err := sendSiacoinsFromFoundationPrimaryAddress(c, subsidyID, foundationPrimaryUnlockConditions, foundationPrimaryKeys, value, types.SiacoinPrecision, address)
		if err != nil {
			return fmt.Errorf("Foundation primary address doesn't contain expected Siacons\ncurrent block height: %v\nsubsidy block height: %v\nerror: %v", currentBH, subsidyBH, err)
		}
	}
	// Check there are no more Siacoins
	err := sendSiacoinsFromFoundationPrimaryAddress(c, subsidyID, foundationPrimaryUnlockConditions, foundationPrimaryKeys, types.NewCurrency64(1), types.NewCurrency64(1), address)
	// errors.Contains() doesn't work and misses an error, we need to compare
	// strings
	if !strings.Contains(err.Error(), errNonExistingOutput.Error()) {
		return fmt.Errorf("Foundation primary address contains unexpected Siacons\ncurrent block height: %v\nsubsidy block height: %v\nerror: %v", currentBH, subsidyBH, err)
	}
	return nil
}

// forwardFoundationSubsidyTwiceCheckReceivedOnce calls
// forwardFoundationSubsidy twice and checks the receiving address receives the
// exact value of Siacoins (once).
func forwardFoundationSubsidyTwiceCheckReceivedOnce(logger *persist.Logger, c *client.Client, forwardSubsidy bool, currentBH, subsidyBH types.BlockHeight, subsidyID types.SiacoinOutputID, foundationPrimaryUnlockConditions types.UnlockConditions, foundationPrimaryKeys []crypto.SecretKey, valueToSend, expectedBalance types.Currency, address types.UnlockHash, receivingAnt *ant.Ant) error {
	err := forwardFoundationSubsidy(logger, c, forwardSubsidy, currentBH, subsidyBH, subsidyID, foundationPrimaryUnlockConditions, foundationPrimaryKeys, valueToSend, address)
	if err != nil {
		return err
	}
	err = forwardFoundationSubsidy(logger, c, forwardSubsidy, currentBH, subsidyBH, subsidyID, foundationPrimaryUnlockConditions, foundationPrimaryKeys, valueToSend, address)
	if err != nil {
		return err
	}
	err = receivingAnt.WaitConfirmedSiacoinBalance(ant.BalanceEquals, expectedBalance, transactionConfirmationTimeout)
	if err != nil {
		return fmt.Errorf("receiving ant doesn't have expected Siacoin balance: %v, block height: %v", err, currentBH)
	}
	return nil
}

// initTest initializes Foundation test and returns a logger and an antfarm.
func initDefaultFoundationAntfarm(logger *persist.Logger, dataDir string, genericAnts int) (*antfarm.AntFarm, error) {
	// Build the Foundation binary
	foundationSiadPath := binariesbuilder.SiadBinaryPath(foundationSiaVersion)
	err := binariesbuilder.StaticBuilder.BuildVersions(logger, forceFoundationBinaryRebuilding, foundationSiaVersion)
	if err != nil {
		return nil, errors.AddContext(err, "can't build Foundation siad binary")
	}

	// Config antfarm with a miner and generic ants.
	antfarmConfig, err := antfarm.NewAntfarmConfig(dataDir, allowLocalIPs, 1, 0, 0, genericAnts)
	if err != nil {
		return nil, errors.AddContext(err, "can't create antfarm config")
	}

	// Update config to use foundation siad dev binaries
	for i := range antfarmConfig.AntConfigs {
		antfarmConfig.AntConfigs[i].SiadPath = foundationSiadPath
	}

	// Create antfarm
	farm, err := antfarm.New(logger, antfarmConfig)
	if err != nil {
		return nil, errors.AddContext(err, "can't create antfarm")
	}

	return farm, nil
}

// initFoundationTest initializes Foundation test and returns a logger and an
// antfarm datadir.
func initFoundationTest(t *testing.T) (*persist.Logger, string) {
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

	// Check UPnP enabled router
	upnpStatus := upnprouter.CheckUPnPEnabled()
	logger.Debugln(upnpStatus)

	return logger, dataDir
}

// sendSiacoinsFromFoundationPrimaryAddress sends Siacoins from the Foundation
// primary multisig address.
func sendSiacoinsFromFoundationPrimaryAddress(c *client.Client, siacoinOutputID types.SiacoinOutputID, foundationUnlockConditions types.UnlockConditions, foundationPrimaryKeys []crypto.SecretKey, amount, minerFee types.Currency, address types.UnlockHash) error {
	// Get current block height
	cg, err := c.ConsensusGet()
	if err != nil {
		return err
	}
	bh := cg.Height

	// Create a transaction
	txn := types.Transaction{
		SiacoinInputs: []types.SiacoinInput{{
			ParentID:         siacoinOutputID,
			UnlockConditions: foundationUnlockConditions,
		}},
		SiacoinOutputs: []types.SiacoinOutput{
			{
				Value:      amount,
				UnlockHash: address,
			},
		},
		MinerFees: []types.Currency{
			minerFee,
		},
		TransactionSignatures: make([]types.TransactionSignature, foundationUnlockConditions.SignaturesRequired),
	}

	// Sign the transaction
	for i := range txn.TransactionSignatures {
		txn.TransactionSignatures[i].ParentID = crypto.Hash(siacoinOutputID)
		txn.TransactionSignatures[i].CoveredFields = types.FullCoveredFields
		txn.TransactionSignatures[i].PublicKeyIndex = uint64(i)
		sig := crypto.SignHash(txn.SigHash(i, bh), foundationPrimaryKeys[i])
		txn.TransactionSignatures[i].Signature = sig[:]
	}

	// Check transaction valid
	err = txn.StandaloneValid(bh)
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

// updateAnts updates ants data directories and starts ants in parallel using
// the given siad path. If dataDirs is nil, ants data directories are not
// changed.
func updateAnts(farm *antfarm.AntFarm, dataDirs []string, siadPath string) error {
	if dataDirs != nil && len(farm.Ants) != len(dataDirs) {
		return fmt.Errorf("Number of ants %d doesn't match number of dataDirs %d", len(farm.Ants), len(dataDirs))
	}
	errChan := make(chan error, len(farm.Ants))
	for i := range farm.Ants {
		a := farm.Ants[i]
		var dir string
		if dataDirs != nil {
			dir = dataDirs[i]
		}
		go func(a *ant.Ant, dataDir string, errChan chan error) {
			if dir != "" {
				a.Config.DataDir = dataDir
			}
			err := a.StartSiad(siadPath)
			errChan <- err
		}(a, dir, errChan)
	}
	for range farm.Ants {
		err := <-errChan
		if err != nil {
			return errors.AddContext(err, "can't configure and start an ant")
		}
	}
	close(errChan)
	return nil
}

// versionAntName creates a version ant name from the version ant index and ant
// version. This is used in creating antfarm ants and in getting them later.
func versionAntName(n int, version string) string {
	return fmt.Sprintf("Version-ant-%d-%s", n, version)
}
