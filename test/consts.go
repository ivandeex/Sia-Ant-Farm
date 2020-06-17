package test

import (
	"fmt"
	"os"
	"path/filepath"

	"gitlab.com/NebulousLabs/Sia/persist"
	"gitlab.com/NebulousLabs/Sia/siatest"
	"gitlab.com/NebulousLabs/fastrand"
)

// TestSiadPath is the siadPath used for testing
const TestSiadPath = "siad-dev"

// RandomLocalhostAddress returns a random localhost address
func RandomLocalhostAddress() string {
	// Get a random port number between 10000 and 20000 for testing
	port := 10000 + fastrand.Intn(10000)
	return fmt.Sprintf("localhost:%v", port)
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
