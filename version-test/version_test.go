package versiontest

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"testing"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/ant"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/antfarm"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/test"
	"gitlab.com/NebulousLabs/Sia/modules"
	"gitlab.com/NebulousLabs/errors"
)

const (
	// skipBuildingBinaries controls whether siad-binaries should be built.
	// Initially this should be set to false, if you want rerun the test and do
	// not need to rebuild the binaries, you can set it to true.
	skipBuildingBinaries = false
)

// buildSiadDevBinaries calls script to get Sia releases starting from v1.3.7,
// adds master, and builds their siad-dev binaries. See scripts/README.md for
// more details.
func buildSiadDevBinaries() error {
	// Call scripts/build-sia-dev-binaries.sh to build required siad-dev
	// binaries.
	err := exec.Command("../scripts/build-sia-dev-binaries.sh").Run()
	if err != nil {
		return errors.AddContext(err, "couldn't build siad-dev binaries")
	}
	return nil
}

// siadBinaryPath returns built siad-dev binary path from the given Sia version
func siadBinaryPath(version string) string {
	return fmt.Sprintf("../upgrade-binaries/Sia-%v-linux-amd64/siad-dev", version)
}

// siaVersions calls script to get Sia releases starting from v1.3.7, adds
// master, and return them as a string slice
func siaVersions() ([]string, error) {
	// Call scripts/get-versions.sh to get Sia releases
	out, err := exec.Command("../scripts/get-versions.sh").Output()
	if err != nil {
		return []string{}, errors.AddContext(err, "couldn't get Sia releases")
	}
	// Convert []byte output to string
	str := string(out)

	// Split output string by new line
	versions := strings.Split(str, "\n")

	// Remove last empty string
	versions = versions[:len(versions)-1]

	// Add master as a version
	versions = append(versions, "master")

	return versions, nil
}

// TestRenterSiadUpdates is a type of version test where renter starts with
// siad-dev version v1.3.7, upgrades iteratively through released Sia version
// to latest master. During each version iteration renter uploads a file and
// downloads and verifies all uploaded files from current and all previous
// versions. Other nodes use the latest siad-dev released version as set in
// go.mod.
func TestRenterSiadUpdates(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Get versions to test.
	// TODO:
	// v1.3.7 and on can be enabled on the Hetzner box when
	// https://gitlab.com/NebulousLabs/Sia-Ant-Farm/-/issues/102
	// is done
	// upgradePathVersions, err := siaVersions()
	// if err != nil {
	// 	t.Fatal(err)
	// }
	upgradePathVersions := []string{"v1.5.0", "master"}

	// Build binaries to test.
	if !skipBuildingBinaries {
		err := buildSiadDevBinaries()
		if err != nil {
			t.Fatal(err)
		}
	}

	// Log upgrade path
	t.Log("Upgrade path:")
	for _, ver := range upgradePathVersions {
		t.Logf("\t%v\n", ver)
	}

	// Configure Antfarm
	dataDir := test.TestDir(t.Name())
	// TODO:
	// antfarmConfig := antfarm.CreateBasicRenterAntfarmConfig(dataDir, false)
	// Testing on external IPs on the Hetzner box can be enabled when
	// https://gitlab.com/NebulousLabs/Sia-Ant-Farm/-/issues/102
	// is done
	antfarmConfig := antfarm.CreateBasicRenterAntfarmConfig(dataDir, true)
	var farm *antfarm.AntFarm

	var uploadedFiles []ant.RenterFile

	for i, version := range upgradePathVersions {
		if i == 0 {
			// First start antfarm with initial renter siad version
			log.Printf("[INFO] Starting antfarm with renter's siad-dev version %v", version)
			antConfigIndex, err := antfarmConfig.GetAntConfigIndexByName(test.RenterAntName)
			if err != nil {
				t.Fatal(err)
			}
			antfarmConfig.AntConfigs[antConfigIndex].SiadConfig.SiadPath = siadBinaryPath(version)
			farm, err = antfarm.New(antfarmConfig)
			if err != nil {
				t.Fatal(err)
			}
			defer farm.Close()
		} else {
			// Update renter to given versions in each following iterations
			log.Printf("[INFO] Updating renter to siad-dev version %v", version)
			renterAnt, err := farm.GetAntByName(test.RenterAntName)
			if err != nil {
				t.Fatal(err)
			}
			err = renterAnt.UpdateSiad(siadBinaryPath(version))
			if err != nil {
				t.Fatal(err)
			}
		}

		// Timeout the test if the renter after update doesn't become upload ready
		renterAnt, err := farm.GetAntByName(test.RenterAntName)
		if err != nil {
			t.Fatal(err)
		}
		err = renterAnt.Jr.WaitForRenterUploadReady()
		if err != nil {
			t.Fatal(err)
		}

		// Upload a file
		renterJob := renterAnt.Jr.NewRenterJob()
		_, err = renterJob.Upload(modules.SectorSize)
		if err != nil {
			t.Fatal(err)
		}

		// Add file to file list
		uploadedFiles = append(uploadedFiles, renterJob.Files[0])

		// Download and verify files
		err = antfarm.DownloadAndVerifyFiles(t, renterAnt, uploadedFiles)
		if err != nil {
			t.Fatal(err)
		}
	}
}
