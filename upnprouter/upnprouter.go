package upnprouter

import (
	"net"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/persist"
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
func CheckUPnPEnabled(logger *persist.Logger) {
	const logInfoPrefix = "INFO upnp-router-check"

	// If we already know that UPnP is not enabled, do not check again
	if !UPnPEnabled {
		logger.Printf("%v: UPnP enabled router was already disabled", logInfoPrefix)
		return
	}

	_, err := upnp.Discover()
	if err != nil {
		UPnPEnabled = false
		logger.Printf("%v: UPnP enabled router is not available and was just disabled: %v", logInfoPrefix, err)
	} else {
		logger.Printf("%v: UPnP enabled router is available", logInfoPrefix)
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
