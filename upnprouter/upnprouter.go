package upnprouter

import (
	"log"
	"net"
	"os"

	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/NebulousLabs/go-upnp"
)

var (
	// UPnPEnabled is a flag to store whether we have UPnP enabled router to
	// save UPnP operations when the router is not enabled
	UPnPEnabled = true
)

// CheckUPnPEnabled checks wheteher there is UPnP enabled router connected and
// sets the flag accordingly
func CheckUPnPEnabled() {
	// If we already know that UPnP is not enabled, do not check again
	if !UPnPEnabled {
		return
	}
	// Gitlab CI doesn't have UPnP enabled router
	if _, ok := os.LookupEnv("GITLAB_CI"); ok {
		UPnPEnabled = false
		log.Println("[INFO] [ant-farm] UPnP enabled router is not available in Gitlab CI")
		return
	}
	_, err := upnp.Discover()
	if err != nil {
		UPnPEnabled = false
		log.Printf("[INFO] [ant-farm] UPnP enabled router is not available: %v", err)
	} else {
		log.Println("[INFO] [ant-farm] UPnP enabled router is available")
	}
}

// ClearPorts clears ports on UPnP enabled router
func ClearPorts(addresses ...*net.TCPAddr) error {
	upnprouter, err := upnp.Discover()
	if err != nil {
		return errors.AddContext(err, "can't discover UPnP enabled router")
	}
	for _, a := range addresses {
		err = upnprouter.Clear(uint16(a.Port))
		if err != nil {
			return err
		}
	}
	return nil
}
