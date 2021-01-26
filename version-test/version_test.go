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
	binariesbuilder "gitlab.com/NebulousLabs/Sia-Ant-Farm/binaries-builder"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/test"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/upnprouter"
	"gitlab.com/NebulousLabs/Sia/build"
	"gitlab.com/NebulousLabs/Sia/modules"
	"gitlab.com/NebulousLabs/Sia/node/api/client"
	"gitlab.com/NebulousLabs/errors"
)

const (
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

	// upgradePathVersionsLimit defines number of versions to include in
	// version tests. It includes the master version.
	upgradePathVersionsLimit = 5

	// allowLocalIPs defines whether we allow ants to use localhost IPs.
	// Default is true. When set to true it is possible to test from Sia v1.5.0
	// on Gitlab CI and on machines without publicly accessible ports and
	// without UPnP enabled router. When set to false, currently it allows to
	// test with external IPs on network with UPnP enabled router.
	allowLocalIPs = true
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

// TestRenterDownloader tests continual downloading of files from the network.
func TestRenterDownloader(t *testing.T) {
	if !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()

	// Prepare test logger
	dataDir := test.TestDir(t.Name())
	testLogger := test.NewTestLogger(t, dataDir)
	defer func() {
		if err := testLogger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Rebuild the master
	master := "master"
	err := binariesbuilder.StaticBuilder.BuildVersions(testLogger, rebuildMaster, master)
	if err != nil {
		t.Fatal(err)
	}

	// Create antfarm config
	antfarmConfig, err := antfarm.NewDefaultRenterAntfarmTestingConfig(dataDir, allowLocalIPs)
	if err != nil {
		t.Fatal(err)
	}

	// Set antfarm config to use the specified branch
	for aci := range antfarmConfig.AntConfigs {
		antfarmConfig.AntConfigs[aci].SiadConfig.SiadPath = binariesbuilder.SiadBinaryPath(master)
	}

	// Start antfarm
	newFarm, err := antfarm.New(testLogger, antfarmConfig)
	farm := newFarm
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := farm.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Get renter ant
	renterAnt, err := farm.GetAntByName(test.RenterAntName)
	if err != nil {
		t.Fatal(err)
	}

	// Timeout the test if the renter doesn't become upload ready
	err = renterAnt.Jr.WaitForRenterUploadReady()
	if err != nil {
		t.Error(err)
	}

	// Calculate storage size
	fileSize := uint64(modules.SectorSize * 4000)

	// Upload a file
	renterJob := renterAnt.Jr.NewRenterJob()
	_, err = renterJob.Upload(fileSize)
	if err != nil {
		t.Fatal(err)
	}

	// Start downloading the file
	downloadsCount := 200
	for i := 0; i < downloadsCount; i++ {
		err = antfarm.DownloadAndVerifyFiles(testLogger, renterAnt, renterJob.Files)
		if err != nil {
			t.Error(err)
			continue
		}
	}
}

// TestRenterUploader tests continual uploading of files from a renter to the
// network.
func TestRenterUploader(t *testing.T) {
	if !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()

	// Prepare test logger
	dataDir := test.TestDir(t.Name())
	testLogger := test.NewTestLogger(t, dataDir)
	defer func() {
		if err := testLogger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Rebuild the master
	master := "master"
	err := binariesbuilder.StaticBuilder.BuildVersions(testLogger, rebuildMaster, master)
	if err != nil {
		t.Fatal(err)
	}

	// Create antfarm config
	antfarmConfig, err := antfarm.NewDefaultRenterAntfarmTestingConfig(dataDir, allowLocalIPs)
	if err != nil {
		t.Fatal(err)
	}

	// Set antfarm config to use the specified branch
	for aci := range antfarmConfig.AntConfigs {
		antfarmConfig.AntConfigs[aci].SiadConfig.SiadPath = binariesbuilder.SiadBinaryPath(master)
	}

	// Start antfarm
	newFarm, err := antfarm.New(testLogger, antfarmConfig)
	farm := newFarm
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := farm.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Get renter ant
	renterAnt, err := farm.GetAntByName(test.RenterAntName)
	if err != nil {
		t.Fatal(err)
	}

	// Timeout the test if the renter doesn't become upload ready
	err = renterAnt.Jr.WaitForRenterUploadReady()
	if err != nil {
		t.Error(err)
	}

	// Calculate storage size
	fileSize := uint64(modules.SectorSize * 4000)
	filesCount := 20
	storageRatio := 1.1 // Add 10% more storage than exact file size * file count
	storageSize := fileSize * uint64(float64(filesCount)*storageRatio)

	// Increase hosts storage folders
	for _, ai := range antfarmConfig.GetHostAntConfigIndices() {
		// Get client
		opts, err := client.DefaultOptions()
		if err != nil {
			t.Fatal(err)
		}
		opts.Address = farm.Ants[ai].APIAddr
		c := client.New(opts)

		// Get storage folder path
		sg, err := c.HostStorageGet()
		if err != nil {
			t.Fatal(err)
		}
		path := sg.Folders[0].Path

		// Resize storage folder
		err = c.HostStorageFoldersResizePost(path, storageSize)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Start uploading files
	renterJob := renterAnt.Jr.NewRenterJob()
	for i := 0; i < filesCount; i++ {
		_, err = renterJob.Upload(fileSize)
		if err != nil {
			t.Error(err)
			continue
		}
	}
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
	upgradePathVersions, err := binariesbuilder.GetReleases(minVersion)
	if err != nil {
		t.Fatal(err)
	}

	// Exclude unwanted releases
	exclVersions := strings.Split(excludeReleasedVersions, ",")
	upgradePathVersions = binariesbuilder.ExcludeVersions(upgradePathVersions, exclVersions)

	// Limit number of versions. '+1' represents the master version
	if len(upgradePathVersions)+1 > upgradePathVersionsLimit {
		upgradePathVersions = upgradePathVersions[len(upgradePathVersions)-upgradePathVersionsLimit+1:]
	}

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
	err = binariesbuilder.StaticBuilder.BuildVersions(logger, rebuildReleaseBinaries, upgradePathVersions...)
	if err != nil {
		t.Fatal(err)
	}
	err = binariesbuilder.StaticBuilder.BuildVersions(logger, rebuildMaster, "master")
	if err != nil {
		t.Fatal(err)
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

	// Prepare test logger
	dataDir := test.TestDir(t.Name())
	testLogger := test.NewTestLogger(t, dataDir)
	defer func() {
		if err := testLogger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Prepare data directory and logger
	antfarmLogger, err := antfarm.NewAntfarmLogger(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := antfarmLogger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Build binary to test
	branch := "master"
	err = binariesbuilder.StaticBuilder.BuildVersions(testLogger, rebuildMaster, branch)
	if err != nil {
		t.Fatal(err)
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
		config.AntConfigs[i].SiadConfig.SiadPath = binariesbuilder.SiadBinaryPath(branch)
	}

	// Create an antfarm
	farm, err := antfarm.New(antfarmLogger, config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := farm.Close(); err != nil {
			antfarmLogger.Errorf("can't close antfarm: %v", err)
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
		// There is no need for timeout, upload itself has its own timeout.
		_, err = renterJob.Upload(modules.SectorSize)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Wait for contracts to renew
	contractsCount := len(config.GetHostAntConfigIndices())
	contractRenewalTimeout := time.Minute * 5
	err = backupRenterAnt.WaitForContractsToRenew(contractsCount, contractRenewalTimeout)
	if err != nil {
		t.Fatal(err)
	}

	// Create a backup
	backupRenterClient, err := backupRenterAnt.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	backupName := "test-backup"
	err = backupRenterClient.RenterCreateBackupPost(backupName)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for backup to finish
	backupUploadTimeout := time.Minute
	backupUploadCheckFrequency := time.Second
	tries := int(backupUploadTimeout / backupUploadCheckFrequency)
	err = build.Retry(tries, backupUploadCheckFrequency, func() error {
		ubs, err := backupRenterClient.RenterBackups()
		if err != nil {
			return errors.AddContext(err, "can't get renter backups")
		}
		var found bool
		for _, ub := range ubs.Backups {
			if ub.Name == backupName {
				found = true
				if ub.UploadProgress == 100 {
					return nil
				}
			}
		}
		if !found {
			return fmt.Errorf("backup with name %s was not found", backupName)
		}

		return errors.New("backup hasn't finished")
	})
	if err != nil {
		t.Fatal(err)
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
	err = restoreRenterAnt.StartSiad(binariesbuilder.SiadBinaryPath(branch))
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

	// Wait for backup to apear in renter backups
	backupAppearTimeout := time.Minute
	backupAppearCheckFrequency := time.Second
	tries = int(backupAppearTimeout / backupAppearCheckFrequency)
	err = build.Retry(tries, backupAppearCheckFrequency, func() error {
		ubs, err := restoreRenterClient.RenterBackups()
		if err != nil {
			return errors.AddContext(err, "can't get renter backups")
		}
		for _, b := range ubs.Backups {
			if b.Name == backupName {
				return nil
			}
		}
		return errors.New("backup was not found")
	})
	if err != nil {
		t.Fatal(err)
	}

	// Restore from backup on restore renter ant
	err = restoreRenterClient.RenterRecoverBackupPost(backupName)
	if err != nil {
		t.Fatal(err)
	}

	// DownloadAndVerifyFiles
	err = antfarm.DownloadAndVerifyFiles(testLogger, restoreRenterAnt, renterJob.Files)
	if err != nil {
		t.Fatal(err)
	}
}

// upgradeTest executes configured renter or hosts upgrade test
func upgradeTest(t *testing.T, testConfig upgradeTestConfig) {
	// Prepare test logger
	dataDir := test.TestDir(t.Name())
	testLogger := test.NewTestLogger(t, dataDir)
	defer func() {
		if err := testLogger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Log upgrade path
	upgradePath := testConfig.upgradePath
	msg := "Upgrade path: "
	msg += strings.Join(upgradePath, " -> ")
	testLogger.Println(msg)

	// Get default Antfarm config
	antfarmLlogger, err := antfarm.NewAntfarmLogger(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := antfarmLlogger.Close(); err != nil {
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

			// Set all ants siad-dev to use the base version, below we set the
			// renter or hosts to initial tested version
			for aci := range antfarmConfig.AntConfigs {
				antfarmConfig.AntConfigs[aci].SiadConfig.SiadPath = binariesbuilder.SiadBinaryPath(testConfig.baseVersion)
			}

			// Initial upgrade renter setup
			if testConfig.upgradeRenter {
				// Set renter to initial tested version
				antfarmConfig.AntConfigs[renterIndex].SiadConfig.SiadPath = binariesbuilder.SiadBinaryPath(version)
				testLogger.Printf("Starting antfarm with renter's siad-dev version %v, all other ants' siad-dev version: %v\n", version, testConfig.baseVersion)
			}
			// Initial hosts setup
			if testConfig.upgradeHosts {
				// Set hosts to initial tested version
				for _, hostIndex := range hostIndices {
					antfarmConfig.AntConfigs[hostIndex].SiadConfig.SiadPath = binariesbuilder.SiadBinaryPath(version)
				}
				testLogger.Printf("Starting antfarm with hosts' siad-dev version %v, all other ants' siad-dev version: %v\n", version, testConfig.baseVersion)
			}

			// Start antfarm
			newFarm, err := antfarm.New(antfarmLlogger, antfarmConfig)
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
				testLogger.Printf("Upgrading renter to siad-dev version %v\n", version)
				err = renterAnt.UpdateSiad(binariesbuilder.SiadBinaryPath(version))
				if err != nil {
					t.Fatal(err)
				}
			}

			// Upgrade hosts
			if testConfig.upgradeHosts {
				testLogger.Printf("Upgrading hosts to siad-version %v\n", version)
				for _, hostIndex := range hostIndices {
					err := farm.Ants[hostIndex].UpdateSiad(binariesbuilder.SiadBinaryPath(version))
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
		err = antfarm.DownloadAndVerifyFiles(testLogger, renterAnt, uploadedFiles)
		if err != nil {
			t.Error(err)
			continue
		}
	}
}
