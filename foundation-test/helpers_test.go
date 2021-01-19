package foundationtest

import (
	"fmt"
	"time"

	"gitlab.com/NebulousLabs/Sia/crypto"
	"gitlab.com/NebulousLabs/Sia/node/api/client"
	"gitlab.com/NebulousLabs/Sia/types"
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

// versionAntName creates a version ant name from the version ant index and ant
// version. This is used in creating antfarm ants and in getting them later.
func versionAntName(n int, version string) string {
	return fmt.Sprintf("Version-ant-%d-%s", n, version)
}
