package versiontest

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/ant"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/antfarm"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/test"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/upnprouter"
	"gitlab.com/NebulousLabs/Sia/modules"
	"gitlab.com/NebulousLabs/errors"
)

const (
	// skipBuildingBinaries controls whether siad-binaries should be built.
	// Initially this should be set to false, if you want rerun the test and do
	// not need to rebuild the binaries, you can set it to true.
	skipBuildingBinaries = false

	// allowLocalIPs defines whether we allow ants to use localhost IPs.
	// Default is true. When set to true it is possible to test from Sia v1.5.0
	// on Gitlab CI and on machines without publicly accessible ports and
	// without UPnP enabled router. When set to false, currently it allows to
	// test with external IPs on network with UPnP enabled router.
	// TODO:
	// Testing on public IPs without UPnP enabled router (e.g. on the Hetzner
	// box) can be enabled when
	// https://gitlab.com/NebulousLabs/Sia-Ant-Farm/-/issues/102
	// is done.
	allowLocalIPs = true
)

// upgradeTestConfig is a struct to create configs for TestUpgrades subtests
type upgradeTestConfig struct {
	testName      string
	upgradeRenter bool
	upgradeHosts  bool
	upgradePath   []string
}

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

// TestUpgrades is test group which contains two version upgrade subtests.
// TestRenterUpgrades is a version test where renter starts with the first
// siad-dev defined in upgradePathVersions, renter upgrades iteratively through
// the released Sia versions to the latest master. TestHostsUpgrades is a
// version test where hosts start with the first siad-dev defined in
// upgradePathVersions, hosts upgrade iteratively through the released Sia
// versions to the latest master. Other ants use the latest siad-dev released
// version as set in go.mod. During each version iteration renter uploads a
// file and downloads and verifies all uploaded files from the current and all
// previous versions.
func TestUpgrades(t *testing.T) {
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

	// Configure tests
	tests := []upgradeTestConfig{
		{testName: "TestRenterUpgrades", upgradeRenter: true, upgradePath: upgradePathVersions},
		{testName: "TestHostsUpgrades", upgradeHosts: true, upgradePath: upgradePathVersions},
	}

	// Check UPnP enabled router to spped up subtests
	upnprouter.CheckUPnPEnabled()

	// Execute tests
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			upgradeTest(t, tt)
		})
	}
}

// upgradeTest executes configured renter or hosts upgrade test
func upgradeTest(t *testing.T, testConfig upgradeTestConfig) {
	// Log upgrade path
	upgradePath := testConfig.upgradePath
	msg := "Upgrade path: "
	msg += strings.Join(upgradePath, " -> ")
	t.Log(msg)

	// Get default Antfarm config
	dataDir := test.TestDir(t.Name())
	antfarmConfig := antfarm.NewDefaultRenterAntfarmTestingConfig(dataDir, allowLocalIPs)

	// Prepare variables for version iterations
	var farm *antfarm.AntFarm
	renterIndex, err := antfarmConfig.GetAntConfigIndexByName(test.RenterAntName)
	if err != nil {
		t.Fatal(err)
	}
	hostIndices := antfarmConfig.GetHostAntConfigIndices()
	var renterAnt *ant.Ant

	// Store uploaded file for each version so that download can be
	// checked on the same version and on all upgraded versions
	var uploadedFiles []ant.RenterFile

	for i, version := range upgradePath {
		if i == 0 {
			// Initial antfarm setup on the first iteration

			// Initial renter setup
			if testConfig.upgradeRenter {
				t.Logf("Starting antfarm with renter's siad-dev version %v\n", version)
				antfarmConfig.AntConfigs[renterIndex].SiadConfig.SiadPath = siadBinaryPath(version)
			}
			// Initial hosts setup
			if testConfig.upgradeHosts {
				t.Logf("Starting antfarm with hosts' siad-dev version %v\n", version)
				for _, hostIndex := range hostIndices {
					antfarmConfig.AntConfigs[hostIndex].SiadConfig.SiadPath = siadBinaryPath(version)
				}
			}

			// Start antfarm
			newFarm, err := antfarm.New(antfarmConfig)
			farm = newFarm
			if err != nil {
				t.Fatal(err)
			}
			defer farm.Close()

			// Get renter ant
			renterAnt, err = farm.GetAntByName(test.RenterAntName)
			if err != nil {
				t.Fatal(err)
			}
		} else {
			// Upgrade step on the following interations

			// Upgrade renter
			if testConfig.upgradeRenter {
				t.Logf("Upgrading renter to siad-dev version %v\n", version)
				err = renterAnt.UpdateSiad(siadBinaryPath(version))
				if err != nil {
					t.Fatal(err)
				}
			}

			// Upgrade hosts
			if testConfig.upgradeHosts {
				t.Logf("Upgrading hosts to siad-version %v\n", version)
				for _, hostIndex := range hostIndices {
					err := farm.Ants[hostIndex].UpdateSiad(siadBinaryPath(version))
					if err != nil {
						t.Fatal(err)
					}
				}
			}
		}

		// Timeout the test if the initial renter or after upgrade doesn't
		// become upload ready
		err = renterAnt.Jr.WaitForRenterUploadReady()
		if err != nil {
			t.Log(err)
			t.Fail()
			return
		}

		// Upload a file
		renterJob := renterAnt.Jr.NewRenterJob()
		_, err = renterJob.Upload(modules.SectorSize)
		if err != nil {
			t.Log(err)
			t.Fail()
			return
		}

		// Add file to file list
		uploadedFiles = append(uploadedFiles, renterJob.Files[0])

		// Download and verify files
		err = antfarm.DownloadAndVerifyFiles(t, renterAnt, uploadedFiles)
		if err != nil {
			t.Log(err)
			t.Fail()
			return
		}
	}
}
