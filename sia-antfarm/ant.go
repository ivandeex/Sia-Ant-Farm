package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/ant"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/upnprouter"
	"gitlab.com/NebulousLabs/Sia/modules"
	"gitlab.com/NebulousLabs/Sia/node/api/client"
	"gitlab.com/NebulousLabs/Sia/types"
	"gitlab.com/NebulousLabs/errors"
)

const (
	// httpClientTimeout defines timeout for http client
	httpClientTimeout = time.Second * 10

	// waitForAntsToSyncFrequency defines how frequently to check if ants are
	// synced
	waitForAntsToSyncFrequency = time.Second
)

// getAddrs returns n free listening ports by leveraging the behaviour of
// net.Listen(":0").  Addresses are returned in the format of ":port"
func getAddrs(n int) ([]string, error) {
	var addrs []string

	for i := 0; i < n; i++ {
		l, err := net.Listen("tcp", ":0") //nolint:gosec
		if err != nil {
			return nil, err
		}
		defer l.Close()
		addrs = append(addrs, fmt.Sprintf(":%v", l.Addr().(*net.TCPAddr).Port))
	}
	return addrs, nil
}

// connectAnts connects two or more ants to the first ant in the slice,
// effectively bootstrapping the antfarm.
func connectAnts(ants ...*ant.Ant) error {
	if len(ants) < 2 {
		return errors.New("you must call connectAnts with at least two ants")
	}
	targetAnt := ants[0]
	opts, err := client.DefaultOptions()
	if err != nil {
		return errors.AddContext(err, "unable to get default client options")
	}
	opts.Address = targetAnt.APIAddr
	opts.Password = targetAnt.Config.APIPassword
	c := client.New(opts)
	for _, ant := range ants[1:] {
		connectQuery := ant.RPCAddr
		addr := modules.NetAddress(ant.RPCAddr)
		if addr.Host() == "" {
			connectQuery = "127.0.0.1" + ant.RPCAddr
		}
		err := c.GatewayConnectPost(modules.NetAddress(connectQuery))
		if err != nil {
			return err
		}
	}
	return nil
}

// antConsensusGroups iterates through all of the ants known to the antFarm
// and returns the different consensus groups that have been formed between the
// ants.
//
// The outer slice is the list of gorups, and the inner slice is a list of ants
// in each group.
func antConsensusGroups(ants ...*ant.Ant) (groups [][]*ant.Ant, err error) {
	opts, err := client.DefaultOptions()
	if err != nil {
		return nil, errors.AddContext(err, "unable to get default client options")
	}
	for _, a := range ants {
		opts.Address = a.APIAddr
		c := client.New(opts)
		cg, err := c.ConsensusGet()
		if err != nil {
			return nil, err
		}
		a.SeenBlocks[cg.Height] = cg.CurrentBlock

		// Compare this ant to all of the other groups. If the ant fits in a
		// group, insert it. If not, add it to the next group.
		found := false
		for gi, group := range groups {
			for i := types.BlockHeight(0); i < 8; i++ {
				id1, exists1 := a.SeenBlocks[cg.Height-i]
				id2, exists2 := group[0].SeenBlocks[cg.Height-i] // no group should have a length of zero
				if exists1 && exists2 && id1 == id2 {
					groups[gi] = append(groups[gi], a)
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			groups = append(groups, []*ant.Ant{a})
		}
	}
	return groups, nil
}

// startAnts starts the ants defined by configs and blocks until every API
// has loaded.
func startAnts(configs ...ant.AntConfig) ([]*ant.Ant, error) {
	var ants []*ant.Ant
	var err error

	// Ensure that, if an error occurs, all the ants that have been started are
	// closed before returning.
	defer func() {
		if err != nil {
			for _, ant := range ants {
				ant.Close()
			}
		}
	}()

	for i, config := range configs {
		cfg, err := parseConfig(config)
		if err != nil {
			return nil, errors.AddContext(err, "unable to parse config")
		}
		// Log config information about the Ant
		fmt.Printf("[INFO] starting ant %v with config: \n", i)
		err = ant.PrintJSON(cfg)
		if err != nil {
			return nil, err
		}

		// Create Ant
		a, err := ant.New(cfg)
		if err != nil {
			return nil, errors.AddContext(err, "unable to create ant")
		}
		defer func() {
			if err != nil {
				a.Close()
			}
		}()

		// Set host netaddress if on local network
		if config.AllowHostLocalNetAddress {
			// Create Sia Client
			c, err := getClient(cfg.APIAddr, cfg.APIPassword)
			if err != nil {
				return nil, err
			}

			// Set netAddress
			netAddress := cfg.HostAddr
			err = c.HostModifySettingPost(client.HostParamNetAddress, netAddress)
			if err != nil {
				return nil, errors.AddContext(err, "couldn't set host's netAddress")
			}
		}

		// Allow renter to rent on hosts on the same IP subnets
		isRenter := false
		for _, j := range config.Jobs {
			if j == "renter" {
				isRenter = true
				break
			}
		}
		if isRenter && config.RenterDisableIPViolationCheck {
			// Disable IP violation check
			r := ant.RenterJob{
				StaticJR: a.Jr,
			}
			err := r.DisableIPViolationCheck()
			if err != nil {
				return nil, err
			}
		}

		ants = append(ants, a)
	}

	return ants, nil
}

// getClient returns http client
func getClient(APIAddr, APIPassword string) (*client.Client, error) {
	opts, err := client.DefaultOptions()
	if err != nil {
		return nil, errors.AddContext(err, "couldn't create client")
	}
	opts.Address = APIAddr
	if APIPassword != "" {
		opts.Password = APIPassword
	}
	return client.New(opts), nil
}

// startJobs starts all the jobs for each ant.
func startJobs(ants ...*ant.Ant) error {
	// first, pull out any constants needed for the jobs
	var spenderAddress *types.UnlockHash
	for _, ant := range ants {
		for _, job := range ant.Config.Jobs {
			if job == "bigspender" {
				addr, err := ant.WalletAddress()
				if err != nil {
					return err
				}
				spenderAddress = addr
			}
		}
	}
	// start jobs requiring those constants
	for _, ant := range ants {
		for _, job := range ant.Config.Jobs {
			if job == "bigspender" {
				ant.StartJob(job)
			}
			if job == "littlesupplier" && spenderAddress != nil {
				err := ant.StartJob(job, *spenderAddress)
				if err != nil {
					return err
				}
				err = ant.StartJob("miner")
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// parseConfig takes an input `config` and fills it with default values if
// required.
func parseConfig(config ant.AntConfig) (ant.AntConfig, error) {
	// if config.SiadConfig.DataDir isn't set, use ioutil.TempDir to create a new
	// temporary directory.
	if config.SiadConfig.DataDir == "" && config.Name == "" {
		tempdir, err := ioutil.TempDir("./antfarm-data", "ant")
		if err != nil {
			return ant.AntConfig{}, err
		}
		config.SiadConfig.DataDir = tempdir
	}

	if config.Name != "" {
		siadir := fmt.Sprintf("./antfarm-data/%v", config.Name)
		err := os.Mkdir(siadir, 0755)
		if err != nil {
			return ant.AntConfig{}, err
		}
		config.SiadConfig.DataDir = siadir
	}

	if config.SiadPath == "" {
		config.SiadPath = "siad-dev"
	}

	// DesiredCurrency and `miner` are mutually exclusive.
	hasMiner := false
	for _, job := range config.Jobs {
		if job == "miner" {
			hasMiner = true
		}
	}
	if hasMiner && config.DesiredCurrency != 0 {
		return ant.AntConfig{}, errors.New("error parsing config: cannot have desired currency with miner job")
	}

	// Set IP address
	ipAddr := "127.0.0.1"
	if !upnprouter.UPnPEnabled && !config.AllowHostLocalNetAddress {
		// UPnP is not enabled and we want hosts to communicate over external
		// IPs (this requires manual port forwarding), i.e. we do not want
		// local addresses for hosts in config
		externalIPAddr, err := myExternalIP()
		if err != nil {
			return ant.AntConfig{}, errors.AddContext(err, "upnp not enabled and failed to get myexternal IP")
		}
		ipAddr = externalIPAddr
	}
	// Automatically generate 5 free operating system ports for the Ant's api,
	// rpc, host, siamux, and siamux websocket addresses
	addrs, err := getAddrs(5)
	if err != nil {
		return ant.AntConfig{}, err
	}
	if config.APIAddr == "" {
		config.APIAddr = ipAddr + addrs[0]
	}
	if config.RPCAddr == "" {
		config.RPCAddr = ipAddr + addrs[1]
	}
	if config.HostAddr == "" {
		config.HostAddr = ipAddr + addrs[2]
	}
	if config.SiaMuxAddr == "" {
		config.SiaMuxAddr = ipAddr + addrs[3]
	}
	if config.SiaMuxWsAddr == "" {
		config.SiaMuxWsAddr = ipAddr + addrs[4]
	}

	return config, nil
}

// myExternalIP discovers the gateway's external IP by querying a centralized
// service, http://myexternalip.com.
func myExternalIP() (string, error) {
	// timeout after 10 seconds
	client := http.Client{Timeout: httpClientTimeout}
	resp, err := client.Get("http://myexternalip.com/raw")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errResp, _ := ioutil.ReadAll(resp.Body)
		return "", errors.New(string(errResp))
	}
	buf, err := ioutil.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return "", err
	}
	if len(buf) == 0 {
		return "", errors.New("myexternalip.com returned a 0 length IP address")
	}
	// trim newline
	return strings.TrimSpace(string(buf)), nil
}

// waitForAntsToSync waits a given timeout for all ants to be synced
func waitForAntsToSync(timeout time.Duration, ants ...*ant.Ant) error {
	log.Println("[INFO] [ant-farm] waiting for all ants to sync")
	for {
		// Check sync status
		groups, err := antConsensusGroups(ants...)
		if err != nil {
			return errors.AddContext(err, "unable to get consensus groups")
		}

		// We have reached ants sync
		if len(groups) == 1 {
			break
		}

		// Wait for jobs stop, timout or sleep
		select {
		case <-ants[0].Jr.StaticTG.StopChan():
			// Jobs were stopped
			return errors.New("jobs were stopped")
		case <-time.After(timeout):
			// We have reached the timeout
			msg := fmt.Sprintf("ants didn't synced in %v", timeout)
			return errors.New(msg)
		case <-time.After(waitForAntsToSyncFrequency):
			// Continue waiting for sync after sleep
		}
	}
	log.Println("[INFO] [ant-farm] all ants are now synced")
	ant.AntSyncWG.Done()
	return nil
}
