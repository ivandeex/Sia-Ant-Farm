package versiontest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

	// excludeReleasedVersions defines comma separated list of Sia releases to
	// be excluded from the version test (TestUpgrades). v1.4.9 is also
	// excluded from the version test, but v1.4.9 didn't have Sia Gitlab
	// release so there is no need to list it here.
	excludeReleasedVersions = "v1.4.10"

	// minVersion defines minimum released Sia version to include in built and
	// tested binaries.
	minVersion = "v1.4.7"

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

	// Get releases from Sia Gitlab repo.
	upgradePathVersions, err := getReleases(minVersion)
	if err != nil {
		t.Fatal(err)
	}

	// Exclude unwanted releases
	exclVersions := strings.Split(excludeReleasedVersions, ",")
	upgradePathVersions = excludeVersions(upgradePathVersions, exclVersions)

	// Get latest release
	latestVersion := upgradePathVersions[len(upgradePathVersions)-1]

	// Prepare logger
	dataDir := test.TestDir(t.Name())
	logger := test.NewTestLogger(t, dataDir)
	defer func() {
		if err := logger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Build binaries to test.
	if rebuildReleaseBinaries {
		err := buildSiad(logger, binariesDir, upgradePathVersions...)
		if err != nil {
			t.Fatal(err)
		}
	}
	if rebuildMaster {
		err := buildSiad(logger, binariesDir, "master")
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

	// Check UPnP enabled router to speed up subtests
	upnpStatus := upnprouter.CheckUPnPEnabled()
	logger.Debugln(upnpStatus)

	// Execute tests
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			upgradeTest(t, tt)
		})
	}
}

// TestRenewContractBackupRestoreSnapshot tests snapshot backup and restore.
// Renter uploads some files, waits for contracts to renew, creates a backup
// that is posted to hosts, and shuts down. A new renter with the same seed is
// started, it restores the backup, downloads and verifies restored files.
func TestRenewContractBackupRestoreSnapshot(t *testing.T) {
	if !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()

	// Prepare data directory and logger
	dataDir := test.TestDir(t.Name())
	logger, err := antfarm.NewAntfarmLogger(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := logger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Build binary to test
	branch := "master"
	if rebuildMaster {
		err := buildSiad(logger, binariesDir, branch)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Create antfarm config
	antfarmDataDir := filepath.Join(dataDir, "antfarm-data")
	config, err := antfarm.NewDefaultRenterAntfarmTestingConfig(antfarmDataDir, true)
	if err != nil {
		t.Fatal(err)
	}

	// Add restore renter to the antfarm config. The ant stays dormant for now,
	// later it will be restarted with renter job, desired currency and with
	// the backup renter seed.
	restoreRenterAntName := "Restore-Renter"
	restoreRenterAntConfig := ant.AntConfig{
		SiadConfig: ant.SiadConfig{
			AllowHostLocalNetAddress:      true,
			APIAddr:                       test.RandomLocalAddress(),
			DataDir:                       filepath.Join(antfarmDataDir, "restore-renter"),
			RenterDisableIPViolationCheck: true,
		},
		Name: restoreRenterAntName,
	}
	config.AntConfigs = append(config.AntConfigs, restoreRenterAntConfig)

	// Update antfarm config with the binary
	for i := range config.AntConfigs {
		config.AntConfigs[i].SiadConfig.SiadPath = siadBinaryPath(branch)
	}

	// Create an antfarm
	farm, err := antfarm.New(logger, config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := farm.Close(); err != nil {
			logger.Errorf("can't close antfarm: %v", err)
		}
	}()

	// Timeout the test if the backup renter doesn't become upload ready
	backupRenterAnt, err := farm.GetAntByName(test.RenterAntName)
	if err != nil {
		t.Fatal(err)
	}
	err = backupRenterAnt.Jr.WaitForRenterUploadReady()
	if err != nil {
		t.Fatal(err)
	}

	// Upload some files
	renterJob := backupRenterAnt.Jr.NewRenterJob()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		_, err = renterJob.Upload(modules.SectorSize)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Get current contracts count
	contractsCount := 5
	backupRenterClient, err := backupRenterAnt.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	rc, err := backupRenterClient.RenterAllContractsGet()
	if err != nil {
		t.Fatal(err)
	}
	if len(rc.ActiveContracts) != contractsCount {
		t.Fatalf("count of active contracts: expected: %d, actual: %d", 5, len(rc.ActiveContracts))
	}
	expiredContracts := len(rc.ExpiredContracts)

	// Wait for contracts to renew
	contractRenewalTimeout := time.Minute * 5
	start := time.Now()
	for {
		// Timeout
		if time.Since(start) > contractRenewalTimeout {
			t.Fatalf("contract renewal not reached within %v timeout", contractRenewalTimeout)
		}

		rec, err := backupRenterClient.RenterExpiredContractsGet()
		if err != nil {
			t.Fatal(err)
		}
		if len(rec.ExpiredContracts) == expiredContracts+contractsCount {
			break
		}

		time.Sleep(time.Second)
	}

	// Create a backup
	backupName := "test-backup"
	err = backupRenterClient.RenterCreateBackupPost(backupName)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for backup to finish
	backupUploadTimeout := time.Minute
	start = time.Now()
backupWaitLoop:
	for {
		// Timeout
		if time.Since(start) > backupUploadTimeout {
			t.Fatalf("backup upload was not finished within %v timeout", backupUploadTimeout)
		}

		ubs, err := backupRenterClient.RenterBackups()
		if err != nil {
			t.Fatal()
		}
		for _, ub := range ubs.Backups {
			if ub.Name == backupName && ub.UploadProgress == 100 {
				break backupWaitLoop
			}
		}

		time.Sleep(time.Second)
	}

	// Close the renter
	err = backupRenterAnt.Close()
	if err != nil {
		t.Fatal(err)
	}

	// Get restore renter ant
	restoreRenterAnt, err := farm.GetAntByName(restoreRenterAntName)
	if err != nil {
		t.Fatal(err)
	}

	// Start a restore renter from scratch using closed renter's seed. Add
	// renter job and desired currency.
	err = restoreRenterAnt.Close()
	if err != nil {
		t.Fatal(err)
	}
	dir := restoreRenterAnt.Config.DataDir
	err = os.RemoveAll(dir)
	if err != nil {
		t.Fatal(err)
	}
	err = os.MkdirAll(dir, 0700)
	if err != nil {
		t.Fatal(err)
	}
	restoreRenterAnt.Jr.StaticWalletSeed = backupRenterAnt.Jr.StaticWalletSeed
	restoreRenterAnt.Config.Jobs = []string{"renter"}
	restoreRenterAnt.Config.DesiredCurrency = 100000
	err = restoreRenterAnt.UpdateSiad(logger, false, siadBinaryPath(branch))
	if err != nil {
		t.Fatal(err)
	}

	// Remove closed renter from the antfarm to prevent connect ants and close
	// antfarm errors
	farm.Ants = farm.Ants[:len(farm.Ants)-2]
	farm.Ants = append(farm.Ants, restoreRenterAnt)

	// Reconnect the restore renter ant. Restore renter ant must be first,
	// because reconnecting already connected ants returns an error.
	antsToConnect := []*ant.Ant{restoreRenterAnt}
	antsToConnect = append(antsToConnect, farm.Ants[:len(farm.Ants)-2]...)
	err = antfarm.ConnectAnts(antsToConnect...)
	if err != nil {
		t.Fatal(err)
	}

	// Timeout the test if the restore renter doesn't become upload ready
	err = restoreRenterAnt.Jr.WaitForRenterUploadReady()
	if err != nil {
		t.Fatal(err)
	}

	// Get restore renter client
	restoreRenterClient, err := restoreRenterAnt.NewClient()
	if err != nil {
		t.Fatal(err)
	}

	// Restore from backup on restore renter
	restoreTimeout := time.Minute * 3
	retryfrequency := time.Second
	err = build.Retry(int(restoreTimeout/retryfrequency), retryfrequency, func() error {
		return restoreRenterClient.RenterRecoverBackupPost(backupName)
	})
	if err != nil {
		t.Fatal(err)
	}

	// DownloadAndVerifyFiles
	err = antfarm.DownloadAndVerifyFiles(t, restoreRenterAnt, renterJob.Files)
	if err != nil {
		t.Fatal(err)
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

	logger, err := antfarm.NewAntfarmLogger(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := logger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	antfarmConfig, err := antfarm.NewDefaultRenterAntfarmTestingConfig(dataDir, allowLocalIPs)
	if err != nil {
		t.Fatal(err)
	}

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
			newFarm, err := antfarm.New(logger, antfarmConfig)
			farm = newFarm
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := farm.Close(); err != nil {
					t.Fatal(err)
				}
			}()

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
				err = renterAnt.UpdateSiad(logger, true, siadBinaryPath(version))
				if err != nil {
					t.Fatal(err)
				}
			}

			// Upgrade hosts
			if testConfig.upgradeHosts {
				t.Logf("Upgrading hosts to siad-version %v\n", version)
				for _, hostIndex := range hostIndices {
					err := farm.Ants[hostIndex].UpdateSiad(logger, true, siadBinaryPath(version))
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
