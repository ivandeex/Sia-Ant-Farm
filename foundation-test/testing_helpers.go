package foundationtest

import (
	"fmt"

	"gitlab.com/NebulousLabs/Sia/crypto"
	"gitlab.com/NebulousLabs/Sia/node/api/client"
	"gitlab.com/NebulousLabs/Sia/types"
	"gitlab.com/NebulousLabs/errors"
)

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
func createSendSiacoinsTransaction(c *client.Client, siacoinInputParentID types.SiacoinOutputID, unlockConditions types.UnlockConditions, amount, minerFee types.Currency, address types.UnlockHash) (types.Transaction, error) {
	// Get current block height
	cg, err := c.ConsensusGet()
	if err != nil {
		return types.Transaction{}, err
	}
	bh := cg.Height

	// Create a transaction
	txn := types.Transaction{
		SiacoinInputs: []types.SiacoinInput{{
			ParentID:         siacoinInputParentID,
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
				ParentID:       crypto.Hash(siacoinInputParentID),
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

	// Check transaction valid
	err = signedTxn.StandaloneValid(bh)
	if err != nil {
		return types.Transaction{}, errors.AddContext(err, "transaction is not valid")
	}

	return signedTxn, nil
}

// sendSiacoinsFromFoundationPrimaryAddress sends Siacoins from the Foundation
// primary multisig address.
func sendSiacoinsFromFoundationPrimaryAddress(c *client.Client, siacoinInputParentID types.SiacoinOutputID, foundationUnlockConditions types.UnlockConditions, foundationPrimaryKeys []crypto.SecretKey, amount, minerFee types.Currency, address types.UnlockHash) error {
	// Get current block height
	cg, err := c.ConsensusGet()
	if err != nil {
		return err
	}
	bh := cg.Height

	// Create a transaction
	txn := types.Transaction{
		SiacoinInputs: []types.SiacoinInput{{
			ParentID:         siacoinInputParentID,
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
		txn.TransactionSignatures[i].ParentID = crypto.Hash(siacoinInputParentID)
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
