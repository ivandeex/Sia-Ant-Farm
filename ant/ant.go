package ant

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/persist"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/upnprouter"
	"gitlab.com/NebulousLabs/Sia/build"
	"gitlab.com/NebulousLabs/Sia/node/api/client"
	"gitlab.com/NebulousLabs/Sia/types"
	"gitlab.com/NebulousLabs/errors"
)

const (
	// updateSiadWarmUpTime defines initial warm-up sleep time for an ant after
	// siad update
	updateSiadWarmUpTime = time.Second * 10
)

// BalanceComparisonOperator defines type for comparison operators enum
type BalanceComparisonOperator string

// BalanceComparisonOperator constants define values for balance comparison
// operators enum
const (
	BalanceLess           BalanceComparisonOperator = "less then"
	BalanceLessOrEqual    BalanceComparisonOperator = "less then or equal"
	BalanceEquals         BalanceComparisonOperator = "equal"
	BalanceGreaterOrEqual BalanceComparisonOperator = "greater then or equal"
	BalanceGreater        BalanceComparisonOperator = "greater then"
)

// Type defines type for ant Type enum
type Type string

// Type constants define values for ant Type enum
const (
	TypeHost    Type = "Host"
	TypeMiner   Type = "Miner"
	TypeRenter  Type = "Renter"
	TypeGeneric Type = "Generic"
)

// AntConfig represents a configuration object passed to New(), used to
// configure a newly created Sia Ant.
type AntConfig struct {
	SiadConfig

	Name            string `json:",omitempty"`
	Jobs            []string
	DesiredCurrency uint64

	InitialWalletSeed string
}

// An Ant is a Sia Client programmed with network user stories. It executes
// these user stories and reports on their successfulness.
type Ant struct {
	staticAntsSyncWG *sync.WaitGroup

	// staticLogger defines a logger an ant should log to. Each ant log message
	// should identify the ant by ant's siad dataDir.
	staticLogger *persist.Logger

	StaticClient *client.Client `json:"-"`

	APIAddr string
	RPCAddr string

	Config AntConfig

	siad *exec.Cmd
	Jr   *JobRunner

	// A variable to track which blocks + heights the sync detector has seen
	// for this ant. The map will just keep growing, but it shouldn't take up a
	// prohibitive amount of space.
	SeenBlocks map[types.BlockHeight]types.BlockID `json:"-"`
}

// clearPorts discovers the UPNP enabled router and clears the ports used by an
// ant before the ant is started.
func clearPorts(config AntConfig) error {
	// Resolve addresses to be cleared
	RPCAddr, err := net.ResolveTCPAddr("tcp", config.RPCAddr)
	if err != nil {
		return errors.AddContext(err, "can't resolve port")
	}

	hostAddr, err := net.ResolveTCPAddr("tcp", config.HostAddr)
	if err != nil {
		return errors.AddContext(err, "can't resolve port")
	}

	siaMuxAddr, err := net.ResolveTCPAddr("tcp", config.SiaMuxAddr)
	if err != nil {
		return errors.AddContext(err, "can't resolve port")
	}

	siaMuxWsAddr, err := net.ResolveTCPAddr("tcp", config.SiaMuxWsAddr)
	if err != nil {
		return errors.AddContext(err, "can't resolve port")
	}

	// Clear ports on the UPnP enabled router
	err = upnprouter.ClearPorts(RPCAddr, hostAddr, siaMuxAddr, siaMuxWsAddr)
	if err != nil {
		return errors.AddContext(err, "can't clear ports")
	}
	return nil
}

// name returns standardized ant name by the given ant type and ant index.
func name(t Type, i int) string {
	return fmt.Sprintf("%s-%d", t, i)
}

// NameGeneric returns standardized ant name of generic ant type by the given
// ant index.
func NameGeneric(i int) string {
	return name(TypeGeneric, i)
}

// NameHost returns standardized ant name of host ant type by the given ant
// index.
func NameHost(i int) string {
	return name(TypeHost, i)
}

// NameMiner returns standardized ant name of miner ant type by the given ant
// index.
func NameMiner(i int) string {
	return name(TypeMiner, i)
}

// NameRenter returns standardized ant name of renter ant type by the given ant
// index.
func NameRenter(i int) string {
	return name(TypeRenter, i)
}

// New creates a new Ant using the configuration passed through `config`.
func New(antsSyncWG *sync.WaitGroup, logger *persist.Logger, config AntConfig) (*Ant, error) {
	// Create ant working dir if it doesn't exist
	// (e.g. ant farm deleted the whole farm dir)
	if _, err := os.Stat(config.DataDir); os.IsNotExist(err) {
		err = os.MkdirAll(config.DataDir, 0700)
		if err != nil {
			return nil, errors.AddContext(err, "can't create ant's data directory")
		}
	}

	// Create ant client
	c, err := newClient(config.APIAddr, config.APIPassword)
	if err != nil {
		return nil, errors.AddContext(err, "can't create a new client")
	}

	// Unforward the ports required for this ant
	upnprouter.CheckUPnPEnabled()
	if upnprouter.UPnPEnabled {
		err := clearPorts(config)
		if err != nil {
			logger.Debugf("%v: can't clear upnp ports for ant: %v", config.DataDir, err)
		}
	}

	// Construct the ant's Siad instance
	siad, err := newSiad(logger, config.SiadConfig)
	if err != nil {
		return nil, errors.AddContext(err, "unable to create new siad process")
	}

	// Ensure siad is always stopped if an error is returned.
	defer func() {
		if err != nil {
			stopSiad(logger, config.DataDir, config.APIAddr, config.APIPassword, siad.Process)
		}
	}()

	ant := &Ant{
		staticAntsSyncWG: antsSyncWG,
		staticLogger:     logger,
		StaticClient:     c,
		APIAddr:          config.APIAddr,
		RPCAddr:          config.RPCAddr,
		Config:           config,
		SeenBlocks:       make(map[types.BlockHeight]types.BlockID),
		siad:             siad,
	}

	j, err := newJobRunner(logger, ant, config.SiadConfig.DataDir, config.InitialWalletSeed)
	if err != nil {
		return nil, errors.AddContext(err, "unable to crate jobrunner")
	}
	ant.Jr = j

	for _, job := range config.Jobs {
		// Here err should be reused (err =) instead of redeclared (err :=), so
		// that defer can catch this error.
		err = ant.StartJob(antsSyncWG, job)
		if err != nil {
			return nil, errors.AddContext(err, "can't start ant's job")
		}
	}

	if config.DesiredCurrency != 0 {
		go j.balanceMaintainer(types.SiacoinPrecision.Mul64(config.DesiredCurrency))
	}

	return ant, nil
}

// newClient creates a new ant http client from the given API address and
// password.
func newClient(APIAddr, APIPassword string) (*client.Client, error) {
	options, err := client.DefaultOptions()
	if err != nil {
		return nil, errors.AddContext(err, "can't create client default options")
	}
	options.Address = APIAddr
	options.Password = APIPassword
	return client.New(options), nil
}

// SprintJSON is a wrapper for json.MarshalIndent
func SprintJSON(v interface{}) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return fmt.Sprintln(string(data)), nil
}

// BlockHeight returns the highest block height seen by the ant.
func (a *Ant) BlockHeight() types.BlockHeight {
	height := types.BlockHeight(0)
	for h := range a.SeenBlocks {
		if h > height {
			height = h
		}
	}
	return height
}

// Close releases all resources created by the ant, including the Siad
// subprocess.
func (a *Ant) Close() error {
	a.staticLogger.Printf("%v: starting to close ant", a.Config.SiadConfig.DataDir)
	err := a.Jr.Stop()
	stopSiad(a.staticLogger, a.Config.DataDir, a.APIAddr, a.Config.APIPassword, a.siad.Process)
	return err
}

// HasRenterTypeJob returns true if the ant has renter type of job (renter or
// autoRenter)
func (a *Ant) HasRenterTypeJob() bool {
	for _, jobName := range a.Config.Jobs {
		jobNameLower := strings.ToLower(jobName)
		if strings.Contains(jobNameLower, "renter") {
			return true
		}
	}
	return false
}

// PrintDebugInfo prints out helpful debug information, arguments define what
// is printed.
func (a *Ant) PrintDebugInfo(contractInfo, hostInfo, renterInfo bool) error {
	var msg string
	client := a.StaticClient
	logger := a.staticLogger
	if contractInfo {
		rc, err := client.RenterAllContractsGet()
		if err != nil {
			return errors.AddContext(err, "can't get all renter contracts")
		}
		msg += "Active Contracts\n"
		for _, c := range rc.ActiveContracts {
			msg += fmt.Sprintf("    ID %v\n", c.ID)
			msg += fmt.Sprintf("    HostPublicKey %v\n", c.HostPublicKey)
			msg += fmt.Sprintf("    GoodForUpload %v\n", c.GoodForUpload)
			msg += fmt.Sprintf("    GoodForRenew %v\n", c.GoodForRenew)
			msg += fmt.Sprintf("    EndHeight %v\n", c.EndHeight)
		}
		msg += "\n"
		msg += "Passive Contracts\n"
		for _, c := range rc.PassiveContracts {
			msg += fmt.Sprintf("    ID %v\n", c.ID)
			msg += fmt.Sprintf("    HostPublicKey %v\n", c.HostPublicKey)
			msg += fmt.Sprintf("    GoodForUpload %v\n", c.GoodForUpload)
			msg += fmt.Sprintf("    GoodForRenew %v\n", c.GoodForRenew)
			msg += fmt.Sprintf("    EndHeight %v\n", c.EndHeight)
		}
		msg += "\n"
		msg += "Refreshed Contracts\n"
		for _, c := range rc.RefreshedContracts {
			msg += fmt.Sprintf("    ID %v\n", c.ID)
			msg += fmt.Sprintf("    HostPublicKey %v\n", c.HostPublicKey)
			msg += fmt.Sprintf("    GoodForUpload %v\n", c.GoodForUpload)
			msg += fmt.Sprintf("    GoodForRenew %v\n", c.GoodForRenew)
			msg += fmt.Sprintf("    EndHeight %v\n", c.EndHeight)
		}
		msg += "\n"
		msg += "Disabled Contracts\n"
		for _, c := range rc.DisabledContracts {
			msg += fmt.Sprintf("    ID %v\n", c.ID)
			msg += fmt.Sprintf("    HostPublicKey %v\n", c.HostPublicKey)
			msg += fmt.Sprintf("    GoodForUpload %v\n", c.GoodForUpload)
			msg += fmt.Sprintf("    GoodForRenew %v\n", c.GoodForRenew)
			msg += fmt.Sprintf("    EndHeight %v\n", c.EndHeight)
		}
		msg += "\n"
		msg += "Expired Contracts\n"
		for _, c := range rc.ExpiredContracts {
			msg += fmt.Sprintf("    ID %v\n", c.ID)
			msg += fmt.Sprintf("    HostPublicKey %v\n", c.HostPublicKey)
			msg += fmt.Sprintf("    GoodForUpload %v\n", c.GoodForUpload)
			msg += fmt.Sprintf("    GoodForRenew %v\n", c.GoodForRenew)
			msg += fmt.Sprintf("    EndHeight %v\n", c.EndHeight)
		}
		msg += "\n"
		msg += "Expired Refreshed Contracts\n"
		for _, c := range rc.ExpiredRefreshedContracts {
			msg += fmt.Sprintf("    ID %v\n", c.ID)
			msg += fmt.Sprintf("    HostPublicKey %v\n", c.HostPublicKey)
			msg += fmt.Sprintf("    GoodForUpload %v\n", c.GoodForUpload)
			msg += fmt.Sprintf("    GoodForRenew %v\n", c.GoodForRenew)
			msg += fmt.Sprintf("    EndHeight %v\n", c.EndHeight)
		}
		msg += "\n"
	}

	if hostInfo {
		hdbag, err := client.HostDbAllGet()
		if err != nil {
			return errors.AddContext(err, "can't get host db all")
		}
		msg += "Active Hosts from HostDB\n"
		for _, host := range hdbag.Hosts {
			hostInfo, err := client.HostDbHostsGet(host.PublicKey)
			if err != nil {
				return errors.AddContext(err, "can't get host db info")
			}
			msg += fmt.Sprintf("    Host: %v\n", host.NetAddress)
			msg += fmt.Sprintf("        score %v\n", hostInfo.ScoreBreakdown.Score)
			msg += fmt.Sprintf("        breakdown %v\n", hostInfo.ScoreBreakdown)
			msg += fmt.Sprintf("        pk %v\n", host.PublicKey)
			msg += fmt.Sprintf("        Accepting Contracts %v\n", host.HostExternalSettings.AcceptingContracts)
			msg += fmt.Sprintf("        Filtered %v\n", host.Filtered)
			msg += fmt.Sprintf("        LastIPNetChange %v\n", host.LastIPNetChange.String())
			msg += "        Subnets\n"
			for _, subnet := range host.IPNets {
				msg += fmt.Sprintf("             %v\n", subnet)
			}
			msg += "\n"
		}
		msg += "\n"
	}

	if renterInfo {
		msg += "Renter Info\n"
		rg, err := client.RenterGet()
		if err != nil {
			return errors.AddContext(err, "can't get all renter info")
		}
		msg += fmt.Sprintf("    CP: %v\n", rg.CurrentPeriod)
		cg, err := client.ConsensusGet()
		if err != nil {
			return errors.AddContext(err, "can't get consensus info")
		}
		msg += fmt.Sprintf("    BH: %v\n", cg.Height)
		settings := rg.Settings
		msg += fmt.Sprintf("    Allowance Funds: %v\n", settings.Allowance.Funds.HumanString())
		fm := rg.FinancialMetrics
		msg += fmt.Sprintf("    Unspent Funds: %v\n", fm.Unspent.HumanString())
		msg += "\n"
	}

	logger.Debugln(msg)
	return nil
}

// StartJob starts the job indicated by `job` after an ant has been
// initialized. Arguments are passed to the job using args.
func (a *Ant) StartJob(antsSyncWG *sync.WaitGroup, job string, args ...interface{}) error {
	if a.Jr == nil {
		return errors.New("ant is not running")
	}

	switch job {
	case "generic":
		go a.Jr.jobGeneric()
	case "miner":
		go a.Jr.blockMining()
	case "host":
		go a.Jr.jobHost()
	case "noAllowanceRenter":
		go a.Jr.renter(walletFull)
	case "renter":
		go a.Jr.renter(allowanceSet)
	case "autoRenter":
		go a.Jr.renter(backgroundJobsStarted)
	case "gateway":
		go a.Jr.gatewayConnectability()
	case "bigspender":
		go a.Jr.bigSpender()
	case "littlesupplier":
		go a.Jr.littleSupplier(args[0].(types.UnlockHash))
	default:
		return errors.New("no such job")
	}

	return nil
}

// StartSiad starts ant using the given siad binary on the previously closed
// ant.
func (a *Ant) StartSiad(siadPath string) error {
	// Update path to new siad binary
	a.Config.SiadConfig.SiadPath = siadPath

	// Construct the ant's Siad instance
	a.staticLogger.Printf("%v: starting new siad process using %v", a.Config.SiadConfig.DataDir, siadPath)
	siad, err := newSiad(a.staticLogger, a.Config.SiadConfig)
	if err != nil {
		return errors.AddContext(err, "unable to create new siad process")
	}
	a.staticLogger.Debugf("%v: siad process started", a.Config.SiadConfig.DataDir)

	// Ensure siad is always stopped if an error is returned.
	defer func() {
		if err != nil {
			stopSiad(a.staticLogger, a.Config.DataDir, a.Config.APIAddr, a.Config.APIPassword, siad.Process)
		}
	}()

	// Update ant's siad process
	a.siad = siad

	// Update ant with recreated newly initialized job runner after siad update
	jr, err := recreateJobRunner(a.Jr)
	if err != nil {
		return errors.AddContext(err, "can't update jobrunner after siad update")
	}
	a.Jr = jr

	// Give a new siad process some warm-up time
	a.staticLogger.Debugf("%v: siad warm-up...", a.Config.SiadConfig.DataDir)
	select {
	case <-a.Jr.StaticTG.StopChan():
		return nil
	case <-time.After(updateSiadWarmUpTime):
	}
	a.staticLogger.Debugf("%v: siad warm-up finished", a.Config.SiadConfig.DataDir)

	// Allow renter to rent on hosts on the same IP subnets
	if a.HasRenterTypeJob() && a.Config.SiadConfig.RenterDisableIPViolationCheck {
		// Set checkforipviolation=false
		values := url.Values{}
		values.Set("checkforipviolation", "false")
		err = a.Jr.staticClient.RenterPost(values)
		if err != nil {
			return errors.AddContext(err, "couldn't set checkforipviolation")
		}
	}

	// Restart jobs
	a.staticLogger.Debugf("%v: restarting ant's jobs", a.Config.SiadConfig.DataDir)
	for _, job := range a.Config.Jobs {
		// Here err should be reused (err =) instead of redeclared (err :=), so
		// that defer can catch this error.
		err = a.StartJob(a.Jr.staticAntsSyncWG, job)
		if err != nil {
			return errors.AddContext(err, "can't restart ant's job")
		}
	}

	// Start balance maintainer if desired currency was set
	if a.Config.DesiredCurrency > 0 {
		go a.Jr.balanceMaintainer(types.SiacoinPrecision.Mul64(a.Config.DesiredCurrency))
	}

	return nil
}

// UpdateSiad updates ant to use the given siad binary.
func (a *Ant) UpdateSiad(siadPath string) error {
	// Stop ant
	a.staticLogger.Debugf("%v: %v", a.Config.DataDir, "closing ant before siad update")
	err := a.Close()
	if err != nil {
		return errors.AddContext(err, "unable to close ant")
	}

	// Start siad
	err = a.StartSiad(siadPath)
	if err != nil {
		return errors.AddContext(err, "can't start ant's siad")
	}

	return nil
}

// WaitConfirmedSiacoinBalance waits until ant wallet confirmed Siacoins meet
// comparison condition.
func (a *Ant) WaitConfirmedSiacoinBalance(cmpOp BalanceComparisonOperator, value types.Currency, timeout time.Duration) error {
	c := a.StaticClient

	frequency := time.Millisecond * 500
	tries := int(timeout / frequency)
	return build.Retry(tries, frequency, func() error {
		wg, err := c.WalletGet()
		if err != nil {
			return errors.AddContext(err, "can't get wallet info")
		}
		cmp := wg.ConfirmedSiacoinBalance.Cmp(value)
		switch {
		case cmpOp == BalanceLess && cmp < 0:
			return nil
		case cmpOp == BalanceLessOrEqual && cmp <= 0:
			return nil
		case cmpOp == BalanceEquals && cmp == 0:
			return nil
		case cmpOp == BalanceGreaterOrEqual && cmp >= 0:
			return nil
		case cmpOp == BalanceGreater && cmp > 0:
			return nil
		default:
			return fmt.Errorf("actual balance %v is expected to be %v expected balance %v", wg.ConfirmedSiacoinBalance, cmpOp, value)
		}
	})
}

// WaitForBlockHeight blocks until the ant reaches the given block height or
// the timeout is reached.
func (a *Ant) WaitForBlockHeight(blockHeight types.BlockHeight, timeout, frequency time.Duration) error {
	// Get client
	c := a.StaticClient

	// Wait for block height
	a.staticLogger.Debugf("%v: waiting for block height %v", a.Config.DataDir, blockHeight)
	tries := int(timeout / frequency)
	err := build.Retry(tries, frequency, func() error {
		cg, err := c.ConsensusGet()
		if err != nil {
			return errors.AddContext(err, "can't get consensus")
		}
		bh := cg.Height
		if bh >= blockHeight {
			return nil
		}

		return fmt.Errorf("block height not reached. Current height: %v, expected height: %v", bh, blockHeight)
	})
	if err != nil {
		er := fmt.Errorf("waiting for block height failed: %v", err)
		a.staticLogger.Debugf("%v: %v", a.Config.DataDir, er)
		return er
	}
	a.staticLogger.Debugf("%v: waiting for block height %v finished", a.Config.DataDir, blockHeight)
	return nil
}

// WaitForContractsToRenew blocks until renter contracts are renewed.
func (a *Ant) WaitForContractsToRenew(contractsCount int, timeout time.Duration) error {
	// Check ant is renter
	if !a.HasRenterTypeJob() {
		return errors.New("The ant doesn't have renter job")
	}
	a.staticLogger.Debugf("%v: waiting for renter contracts to renew", a.Config.SiadConfig.DataDir)

	// Get current active contracts
	rc, err := a.Jr.staticClient.RenterContractsGet()
	if err != nil {
		return errors.AddContext(err, "can't get renter contracts")
	}
	if len(rc.ActiveContracts) != contractsCount {
		return fmt.Errorf("count of active contracts: expected: %d, actual: %d", contractsCount, len(rc.ActiveContracts))
	}

	// Get contracts end height
	var contractsEndHeight types.BlockHeight
	for _, c := range rc.ActiveContracts {
		h := c.EndHeight
		if h > contractsEndHeight {
			contractsEndHeight = h
		}
	}

	// Wait for block height after all active contracts end
	start := time.Now()
	err = a.WaitForBlockHeight(contractsEndHeight+1, timeout, time.Second)
	if err != nil {
		return errors.AddContext(err, "waiting for contracts end height failed")
	}

	// Wait for new contracts form
	newContractsTimeout := timeout - time.Since(start)
	frequency := time.Second
	tries := int(newContractsTimeout/frequency) + 1
	err = build.Retry(tries, frequency, func() error {
		rc, err := a.Jr.staticClient.RenterContractsGet()
		if err != nil {
			return errors.AddContext(err, "can't get renter contracts")
		}
		if len(rc.ActiveContracts) != contractsCount {
			return fmt.Errorf("count of active contracts: expected: %d, actual: %d", contractsCount, len(rc.ActiveContracts))
		}
		return nil
	})
	if err != nil {
		er := fmt.Errorf("waiting for block contracts renew failed: %v", err)
		a.staticLogger.Debugf("%v: %v", a.Config.DataDir, er)
		return er
	}

	a.staticLogger.Debugf("%v: waiting for renter contracts to renew finished", a.Config.SiadConfig.DataDir)
	return nil
}

// WaitForRenterWorkersCooldown blocks until renter workers price tables are
// updated and none of renter workers are on cooldown.
func (a *Ant) WaitForRenterWorkersCooldown(timeout time.Duration) error {
	a.staticLogger.Debugf("%v: waiting for renter workers cooldown...", a.Config.SiadConfig.DataDir)
	start := time.Now()

	// Wait for renter workers price table updates
	updateTimes := make(map[types.FileContractID]time.Time)
	rwg, err := a.Jr.staticClient.RenterWorkersGet()
	if err != nil {
		return errors.AddContext(err, "can't get renter workers info")
	}
	for _, w := range rwg.Workers {
		updateTimes[w.ContractID] = w.PriceTableStatus.UpdateTime
	}
	frequency := time.Second
	tries := int(timeout/frequency) + 1
	err = build.Retry(tries, frequency, func() error {
		rwg, err := a.Jr.staticClient.RenterWorkersGet()
		if err != nil {
			return errors.AddContext(err, "can't get renter workers info")
		}
		for _, w := range rwg.Workers {
			ut := w.PriceTableStatus.UpdateTime
			now := time.Now()
			if ut0, ok := updateTimes[w.ContractID]; ok && (ut == ut0 || ut.Before(now)) {
				return fmt.Errorf("renter workers price table was not updated")
			}
		}
		return nil
	})
	if err != nil {
		er := fmt.Errorf("waiting for renter workers pricetable updates reached %v timeout: %v", timeout, err)
		a.staticLogger.Errorf("%v: %v", a.Config.SiadConfig.DataDir, er)
		return er
	}

	// Give a little time for possible cooldowns to start
	select {
	case <-a.Jr.StaticTG.StopChan():
		return nil
	case <-time.After(time.Second):
	}

	// Wait for renter workers cooldown
	elapsed := time.Since(start)
	timeout = timeout - elapsed
	tries = int(timeout/frequency) + 1
	err = build.Retry(tries, frequency, func() error {
		rwg, err := a.Jr.staticClient.RenterWorkersGet()
		if err != nil {
			return errors.AddContext(err, "can't get renter workers info")
		}
		if rwg.TotalDownloadCoolDown+rwg.TotalMaintenanceCoolDown+rwg.TotalUploadCoolDown > 0 {
			return fmt.Errorf("there are %d renter workers on download cooldown, %d on maintenance cooldown, %d on upload cooldown",
				rwg.TotalDownloadCoolDown, rwg.TotalMaintenanceCoolDown, rwg.TotalUploadCoolDown)
		}
		return nil
	})
	if err != nil {
		er := fmt.Errorf("waiting for renter workers cooldown reached %v timeout: %v", timeout, err)
		a.staticLogger.Errorf("%v: %v", a.Config.SiadConfig.DataDir, er)
		return er
	}

	a.staticLogger.Debugf("%v: waiting for renter workers cooldown finished in %v", a.Config.SiadConfig.DataDir, time.Since(start))
	return nil
}

// WalletAddress returns a wallet address that this ant can receive coins on.
func (a *Ant) WalletAddress() (*types.UnlockHash, error) {
	if a.Jr == nil {
		return nil, errors.New("ant is not running")
	}

	addressGet, err := a.Jr.staticClient.WalletAddressGet()
	if err != nil {
		return nil, err
	}

	return &addressGet.Address, nil
}
