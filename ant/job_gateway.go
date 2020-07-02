package ant

import (
	"log"
	"time"
)

// gatewayConnectability will print an error to the log if the node has zero
// peers at any time.
func (j *jobRunner) gatewayConnectability() {
	j.staticTG.Add()
	defer j.staticTG.Done()

	// Wait for ants to be synced if the wait group was set
	AntSyncWG.Wait()

	// Initially wait a while to give the other ants some time to spin up.
	select {
	case <-j.staticTG.StopChan():
		return
	case <-time.After(time.Minute):
	}

	for {
		// Wait 30 seconds between iterations.
		select {
		case <-j.staticTG.StopChan():
			return
		case <-time.After(time.Second * 30):
		}

		// Count the number of peers that the gateway has. An error is reported
		// for less than two peers because the gateway is likely connected to
		// itself.
		gatewayInfo, err := j.staticClient.GatewayGet()
		if err != nil {
			log.Printf("[ERROR] [gateway] [%v] error when calling /gateway: %v\n", j.staticSiaDirectory, err)
		}
		if len(gatewayInfo.Peers) < 2 {
			log.Printf("[ERROR] [gateway] [%v] ant has less than two peers: %v\n", j.staticSiaDirectory, gatewayInfo.Peers)
		}
	}
}
