package binariesbuilder

import (
	"sync"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/persist"
	"gitlab.com/NebulousLabs/errors"
)

const (
	// BinariesDir defines path to store built siad binaries.
	BinariesDir = "../upgrade-binaries"
)

// StaticBuilder defines a struct with methods to build different siad binary
// versions for multiple tests. Versions used by multiple tests are build
// first.
var StaticBuilder builder

// builder defines a struct to be used by staticBuilder
type builder struct {
	sync.Mutex
	// versionMap defines map with version strings as keys and version status
	// as values.
	versionMap map[string]versionStatus
	building   bool
}

// versionStatus defines struct to collect caller channels to be notified about
// version result (error) and the error itself.
type versionStatus struct {
	callerChans []chan error
	logger      *persist.Logger
	err         error
}

func init() {
	StaticBuilder.Lock()
	// Init static builder with an empty version map.
	StaticBuilder.versionMap = map[string]versionStatus{}
	StaticBuilder.Unlock()
}

// BuildVersions defines a static builder method to request to build siad
// binaries and blocks until all the requested versions are built. This method
// is thread safe and can be called concurrently from parallel running tests.
// If several tests request to build the same siad version, the version is
// built just once.
func (b *builder) BuildVersions(logger *persist.Logger, binariesDir string, versions ...string) error {
	// Request to build each version
	var chans []chan error
	for _, v := range versions {
		ch := make(chan error)
		b.managedBuildVersion(logger, binariesDir, v, ch)
		chans = append(chans, ch)
	}

	// Wait for each version to be built
	for _, ch := range chans {
		err := <-ch
		if err != nil {
			return errors.AddContext(err, "can't build a siad binary")
		}
	}
	return nil
}

// managedBuildVersion returns already built version results or requests worker
// to build the specific version.
func (b *builder) managedBuildVersion(logger *persist.Logger, binariesDir string, version string, ch chan error) {
	b.Lock()
	defer b.Unlock()

	// Return version build result if the version is already built.
	if s, ok := b.versionMap[version]; ok && len(s.callerChans) == 0 {
		go func(ch chan error) {
			ch <- s.err
		}(ch)
		return
	}

	// Add logger and waiting channel to the version status
	s := b.versionMap[version]
	s.callerChans = append(s.callerChans, ch)
	s.logger = logger
	b.versionMap[version] = s

	// Start the build worker.
	go b.threadedUpdateBuilds(binariesDir)
}

// threadedUpdateBuilds selects the versions to be built from version map,
// builds the versions and updates version build statuses. It makes sure that
// at most only one build worker is running at any time.
func (b *builder) threadedUpdateBuilds(binariesDir string) {
	// Allow max 1 build worker to be active
	b.Lock()
	if b.building {
		b.Unlock()
		return
	}

	// Build all versions in the queue. Unlock the builder when building, keep
	// the lock while selecting a version or updating the version status.
	for {
		// Select version to build and caller logger. Priority has a version
		// with the most waiting callers.
		var logger *persist.Logger
		var versionToBuild string
		var maxCallers int
		for k, v := range b.versionMap {
			if l := len(v.callerChans); l > maxCallers {
				maxCallers = l
				versionToBuild = k
				logger = v.logger
			}
		}
		// Return if there are no more versions to be built in the current
		// queue.
		if versionToBuild == "" {
			b.building = false
			b.Unlock()
			return
		}

		b.building = true
		b.Unlock()

		// Build a version without a lock, so that callers can add new requests.
		err := buildSiad(logger, binariesDir, versionToBuild)

		b.Lock()
		// Notify callers.
		for _, ch := range b.versionMap[versionToBuild].callerChans {
			go func(ch chan error, err error) {
				ch <- err
			}(ch, err)
		}
		// Clear version callers, save version build result.
		s := versionStatus{
			callerChans: []chan error{},
			err:         err,
		}
		b.versionMap[versionToBuild] = s
	}
}
