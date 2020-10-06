package versiontest

import (
	"fmt"
	"strings"
	"testing"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/ant"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/antfarm"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/test"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/upnprouter"
	"gitlab.com/NebulousLabs/Sia/build"
	"gitlab.com/NebulousLabs/Sia/modules"
)

const (
	// binariesDir defines path where build binaries should be stored. If the
	// path is set as relative, it is relative to Sia-Ant-Farm/version-test
	// directory.
	binariesDir = "../upgrade-binaries"

	// minVersion defines minimum released Sia version to include in built and
	// tested binaries.
	// TODO: Bring minVersion to v1.3.7
	minVersion = "v1.4.8"

	// rebuildReleaseBinaries defines whether the release siad binaries should
	// be rebuilt. It can be set to false when rerunning the test(s) on already
	// built binaries.
	rebuildReleaseBinaries = true

	// rebuildMaster defines whether the newest Sia master siad binary should
	// be rebuilt. It can be set to false when rerunning the test(s) on already
	// build binary.
	rebuildMaster = true

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
	allowLocalIPs = false
)

// upgradeTestConfig is a struct to create configs for TestUpgrades subtests
type upgradeTestConfig struct {
	testName      string
	upgradeRenter bool
	upgradeHosts  bool
	upgradePath   []string

	// baseVersion defines Sia version of the ants that do not follow upgrade
	// path. It could be e.g. latest released version (e.g. v1.5.0) or the
	// latest master.
	baseVersion string
}

// siadBinaryPath returns built siad-dev binary path from the given Sia version
func siadBinaryPath(version string) string {
	return fmt.Sprintf("../upgrade-binaries/Sia-%v-linux-amd64/siad-dev", version)
}

// TestUpgrades is test group which contains two types of version upgrade
// subtests. TestRenterUpgrades is a version test type where renter starts with
// the first siad-dev defined in upgradePathVersions, renter upgrades
// iteratively through the released Sia versions to the latest master.
// TestHostsUpgrades is a version test type where hosts start with the first
// siad-dev defined in upgradePathVersions, hosts upgrade iteratively through
// the released Sia versions to the latest master. Other ants use either the
// latest siad-dev released version (WithBaseLatestRelease test postfix) or the
// latest master (WithBaseLatestMaster test postfix). During each version
// iteration renter uploads a file and downloads and verifies all uploaded
// files from the current and all previous versions.
func TestUpgrades(t *testing.T) {
	if !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()

	// Get versions to test.
	// TODO:
	// v1.3.7 and on can be enabled on the Hetzner box when
	// https://gitlab.com/NebulousLabs/Sia-Ant-Farm/-/issues/102
	// is done
	upgradePathVersions, err := getReleases(minVersion)
	if err != nil {
		t.Fatal(err)
	}
	latestVersion := upgradePathVersions[len(upgradePathVersions)-1]

	// Build binaries to test.
	if rebuildReleaseBinaries {
		err := buildSiad(binariesDir, upgradePathVersions...)
		if err != nil {
			t.Fatal(err)
		}
	}
	if rebuildMaster {
		err := buildSiad(binariesDir, "master")
		if err != nil {
			t.Fatal(err)
		}
	}

	// Add master to upgrade path
	upgradePathVersions = append(upgradePathVersions, "master")

	// Configure tests
	tests := []upgradeTestConfig{
		{testName: "TestRenterUpgradesWithBaseLatestRelease", upgradeRenter: true, upgradePath: upgradePathVersions, baseVersion: latestVersion},
		{testName: "TestRenterUpgradesWithBaseLatestMaster", upgradeRenter: true, upgradePath: upgradePathVersions, baseVersion: "master"},
		{testName: "TestHostsUpgradesWithBaseLatestRelease", upgradeHosts: true, upgradePath: upgradePathVersions, baseVersion: latestVersion},
		{testName: "TestHostsUpgradesWithBaseLatestMaster", upgradeHosts: true, upgradePath: upgradePathVersions, baseVersion: "master"},
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
		t.Error(err)
		return
	}
	hostIndices := antfarmConfig.GetHostAntConfigIndices()
	var renterAnt *ant.Ant

	// Store uploaded file for each version so that download can be
	// checked on the same version and on all upgraded versions
	var uploadedFiles []ant.RenterFile

	for i, version := range upgradePath {
		if i == 0 {
			// Initial antfarm setup on the first iteration

			// Set all ants siad-dev to use the base version, below we set the
			// renter or hosts to initial tested version
			for aci := range antfarmConfig.AntConfigs {
				antfarmConfig.AntConfigs[aci].SiadConfig.SiadPath = siadBinaryPath(testConfig.baseVersion)
			}

			// Initial upgrade renter setup
			if testConfig.upgradeRenter {
				// Set renter to initial tested version
				antfarmConfig.AntConfigs[renterIndex].SiadConfig.SiadPath = siadBinaryPath(version)
				t.Logf("Starting antfarm with renter's siad-dev version %v, all other ants' siad-dev version: %v\n", version, testConfig.baseVersion)
			}
			// Initial hosts setup
			if testConfig.upgradeHosts {
				// Set hosts to initial tested version
				for _, hostIndex := range hostIndices {
					antfarmConfig.AntConfigs[hostIndex].SiadConfig.SiadPath = siadBinaryPath(version)
				}
				t.Logf("Starting antfarm with hosts' siad-dev version %v, all other ants' siad-dev version: %v\n", version, testConfig.baseVersion)
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
			t.Error(err)
			continue
		}

		// Upload a file
		renterJob := renterAnt.Jr.NewRenterJob()
		_, err = renterJob.Upload(modules.SectorSize)
		if err != nil {
			t.Error(err)
			continue
		}

		// Add file to file list
		uploadedFiles = append(uploadedFiles, renterJob.Files[0])

		// Download and verify files
		err = antfarm.DownloadAndVerifyFiles(t, renterAnt, uploadedFiles)
		if err != nil {
			t.Error(err)
			continue
		}
	}
}
