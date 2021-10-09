package ant

import (
	"time"

	"go.sia.tech/siad/types"
)

const (
	// balanceIncreaseCheckFrequency defines how often the wallet will be
	// checked for balance increases
	balanceIncreaseCheckFrequency = time.Second * 100

	// balanceIncreaseCheckWarmup defines initial interval in which the miner
	// balance will not yet be checked
	balanceIncreaseCheckWarmup = time.Minute * 2

	// blockCheckFrequency defines interval in which new block generation will
	// be checked
	blockCheckFrequency = time.Millisecond * 500
)

// blockMining indefinitely mines blocks.  If more than 100
// seconds passes before the wallet has received some amount of currency, this
// job will print an error.
func (j *JobRunner) blockMining() {
	err := j.StaticTG.Add()
	if err != nil {
		j.staticLogger.Errorf("%v: can't add thread group: %v", j.staticDataDir, err)
		return
	}
	defer j.StaticTG.Done()

	// Note:
	// blockMining doesn't wait for WaitForSync

	err = j.staticClient.MinerStartGet()
	if err != nil {
		j.staticLogger.Errorf("%v: can't start miner: %v", j.staticDataDir, err)
		return
	}

	// Get block frequency and set miner sleep time.
	cg, err := j.staticClient.ConsensusGet()
	if err != nil {
		j.staticLogger.Errorf("%v: can't get consensus info: %v", j.staticDataDir, err)
		return
	}
	blockFrequency := cg.BlockFrequency // seconds per block
	minerSleepTime := time.Second * time.Duration(blockFrequency/2)

	// After mining a block, sleep half block frequency time, so that mining is
	// more consistent. Every 100 seconds, verify that the balance has
	// increased.
	var lastBH types.BlockHeight
	var lastBalance types.Currency
	lastBallanceCheck := time.Now()
	start := time.Now()
	for {
		// Wait before check.
		select {
		case <-j.StaticTG.StopChan():
			return
		case <-time.After(blockCheckFrequency):
		}

		// Get consensus to get block height.
		cg, err := j.staticClient.ConsensusGet()
		if err != nil {
			j.staticLogger.Errorf("%v: can't get consensus info: %v", j.staticDataDir, err)
			continue
		}

		// Check if we just mined a block
		if cg.Height > lastBH {
			lastBH = cg.Height

			// Turn off the miner after mining a block.
			err = j.staticClient.MinerStopGet()
			if err != nil {
				j.staticLogger.Errorf("%v: can't stop miner: %v", j.staticDataDir, err)
				continue
			}

			// Sleep half of the block frequency interval.
			select {
			case <-j.StaticTG.StopChan():
				return
			case <-time.After(minerSleepTime):
			}

			// Turn on the miner.
			err = j.staticClient.MinerStartGet()
			if err != nil {
				j.staticLogger.Errorf("%v: can't start miner: %v", j.staticDataDir, err)
				continue
			}
		}

		// Check that miner balance is increasing.
		if time.Since(lastBallanceCheck) > balanceIncreaseCheckFrequency {
			walletInfo, err := j.staticClient.WalletGet()
			if err != nil {
				j.staticLogger.Errorf("%v: can't get wallet info: %v", j.staticDataDir, err)
				continue
			}
			if walletInfo.ConfirmedSiacoinBalance.Cmp(lastBalance) > 0 {
				j.staticLogger.Printf("%v: Blockmining job succeeded", j.staticDataDir)
				lastBalance = walletInfo.ConfirmedSiacoinBalance
			} else if time.Since(start) > balanceIncreaseCheckWarmup {
				j.staticLogger.Errorf("%v: it took too long to receive new funds in miner job", j.staticDataDir)
			}
			lastBallanceCheck = time.Now()
		}
	}
}
