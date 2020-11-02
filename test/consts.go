package test

import (
	"fmt"
	"os"
	"path/filepath"

	"gitlab.com/NebulousLabs/Sia/persist"
	"gitlab.com/NebulousLabs/Sia/siatest"
	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/NebulousLabs/fastrand"
)

const (
	// TestSiadFilename is the siad file name in PATH used for testing
	TestSiadFilename = "siad-dev"

	// RenterAntName defines name of the renter ant
	RenterAntName = "Renter"
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

// RandomLocalAddress returns a random local 127.0.0.1 address
func RandomLocalAddress() string {
	// Get a random port number between 10000 and 20000 for testing
	port := 10000 + fastrand.Intn(10000)
	return fmt.Sprintf("127.0.0.1:%v", port)
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
	err = os.MkdirAll(path, persist.DefaultDiskPermissionsTest)
	if err != nil {
		panic(err)
	}
	return path
}
