package ant

import (
	"time"
)

const (
	// gatewayConnectabilityCheckInterval defines how often the gateway
	// connectability loop will run
	gatewayConnectabilityCheckInterval = time.Second * 30
)

// gatewayConnectability will print an error to the log if the node has zero
// peers at any time.
func (j *JobRunner) gatewayConnectability() {
	err := j.StaticTG.Add()
	if err != nil {
		return
	}
	defer j.StaticTG.Done()

	// Wait for ants to be synced if the wait group was set
	synced := j.waitForAntsSync()
	if !synced {
		return
	}

	// Check the gateway connections in a loop
	for {
		// Start with a sleep to allow other ants to start up before the first
		// check. This also eliminates the need for an error sleep.
		select {
		case <-j.StaticTG.StopChan():
			return
		case <-time.After(gatewayConnectabilityCheckInterval):
		}

		// Count the number of peers that the gateway has. An error is reported
		// for less than two peers because the gateway is likely connected to
		// itself.
		gatewayInfo, err := j.StaticClient.GatewayGet()
		if err != nil {
			j.staticLogger.Errorf("%v: error when calling /gateway: %v", j.staticDataDir, err)
			continue
		}
		if len(gatewayInfo.Peers) < 2 {
			j.staticLogger.Errorf("%v: ant has less than two peers: %v", j.staticDataDir, gatewayInfo.Peers)
			continue
		}
	}
}
