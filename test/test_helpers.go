package test

import (
	"fmt"
	"os"
	"path/filepath"

	siapersist "go.sia.tech/siad/persist"
	"go.sia.tech/siad/siatest"
	"gitlab.com/NebulousLabs/errors"
)

const (
	// TestSiadFilename is the siad file name in PATH used for testing
	TestSiadFilename = "siad-dev"

	// RenterAntName defines name of the renter ant
	RenterAntName = "Renter"

	// WalletSeed1 stores a wallet seed, that can be reused in tests
	WalletSeed1 = "vipers happens bias nodes names nirvana volcano stylishly smog oust gutter network lava earth distance spying hijack aggravate oust byline nostril patio sneeze uttered phone ghetto history ember adhesive"

	// WalletSeed1Address1 stores an address belonging to the wallet
	// initialized with WalletSeed1
	WalletSeed1Address1 = "c34636e3a92cf8639d5a4eaf03663348d3d2e8f2a39143a2a902fa9c715c5a4d284c444d8e6b"
)

// AbsoluteSiadPath returns default absolute siad path in local or Gitlab CI
// environments
func AbsoluteSiadPath() (string, error) {
	path, err := filepath.Abs(RelativeSiadPath())
	if err != nil {
		return "", errors.AddContext(err, "")
	}
	return path, nil
}

// AntDirs creates temporary test directories for numAnt directories. This
// should only every be called once per test. Otherwise it will delete the
// directories again.
func AntDirs(dataDir string, numAnts int) ([]string, error) {
	antDirs := []string{}
	for i := 0; i < numAnts; i++ {
		path := filepath.Join(dataDir, fmt.Sprintf("ant_%v", i))
		antDirs = append(antDirs, path)

		// Clean ant dirs
		if err := os.RemoveAll(path); err != nil {
			return []string{}, errors.AddContext(err, "can't remove ant data directory")
		}
		if err := os.MkdirAll(path, 0700); err != nil {
			return []string{}, errors.AddContext(err, "can't create ant data directory")
		}
	}
	return antDirs, nil
}

// RelativeSiadPath returns default relative siad path in local or Gitlab CI
// environments
func RelativeSiadPath() string {
	// Check if executing on Gitlab CI
	if _, ok := os.LookupEnv("GITLAB_CI"); ok {
		return "../.cache/bin/siad-dev"
	}
	return "../../../../../bin/siad-dev"
}

// TestDir creates a temporary testing directory. This should only every be
// called once per test. Otherwise it will delete the directory again.
func TestDir(testName string) string {
	path := filepath.Join(siatest.SiaTestingDir, "ant-farm", testName)
	err := os.RemoveAll(path)
	if err != nil {
		panic(err)
	}
	err = os.MkdirAll(path, siapersist.DefaultDiskPermissionsTest)
	if err != nil {
		panic(err)
	}
	return path
}
