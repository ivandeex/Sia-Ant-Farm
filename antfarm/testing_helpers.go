package antfarm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/ant"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/test"
	"gitlab.com/NebulousLabs/Sia/modules"
	"gitlab.com/NebulousLabs/errors"
)

// NewDefaultRenterAntfarmTestingConfig creates default basic antfarm config
// for running renter tests
func NewDefaultRenterAntfarmTestingConfig(dataDir string, allowLocalIPs bool) AntfarmConfig {
	antFarmAddr := test.RandomLocalAddress()
	antFarmDir := filepath.Join(dataDir, "antfarm-data")
	antDirs := test.AntDirs(dataDir, 7)
	config := AntfarmConfig{
		ListenAddress: antFarmAddr,
		DataDir:       antFarmDir,
		AntConfigs: []ant.AntConfig{
			{
				SiadConfig: ant.SiadConfig{
					AllowHostLocalNetAddress: allowLocalIPs,
					APIAddr:                  test.RandomLocalAddress(),
					DataDir:                  antDirs[0],
					SiadPath:                 test.TestSiadFilename,
				},
				Jobs: []string{"gateway", "miner"},
			},
			{
				SiadConfig: ant.SiadConfig{
					AllowHostLocalNetAddress: allowLocalIPs,
					APIAddr:                  test.RandomLocalAddress(),
					DataDir:                  antDirs[1],
					SiadPath:                 test.TestSiadFilename,
				},
				Jobs:            []string{"host"},
				DesiredCurrency: 100000,
			},
			{
				SiadConfig: ant.SiadConfig{
					AllowHostLocalNetAddress: allowLocalIPs,
					APIAddr:                  test.RandomLocalAddress(),
					DataDir:                  antDirs[2],
					SiadPath:                 test.TestSiadFilename,
				},
				Jobs:            []string{"host"},
				DesiredCurrency: 100000,
			},
			{
				SiadConfig: ant.SiadConfig{
					AllowHostLocalNetAddress: allowLocalIPs,
					APIAddr:                  test.RandomLocalAddress(),
					DataDir:                  antDirs[3],
					SiadPath:                 test.TestSiadFilename,
				},
				Jobs:            []string{"host"},
				DesiredCurrency: 100000,
			},
			{
				SiadConfig: ant.SiadConfig{
					AllowHostLocalNetAddress: allowLocalIPs,
					APIAddr:                  test.RandomLocalAddress(),
					DataDir:                  antDirs[4],
					SiadPath:                 test.TestSiadFilename,
				},
				Jobs:            []string{"host"},
				DesiredCurrency: 100000,
			},
			{
				SiadConfig: ant.SiadConfig{
					AllowHostLocalNetAddress: allowLocalIPs,
					APIAddr:                  test.RandomLocalAddress(),
					DataDir:                  antDirs[5],
					SiadPath:                 test.TestSiadFilename,
				},
				Jobs:            []string{"host"},
				DesiredCurrency: 100000,
			},
			{
				SiadConfig: ant.SiadConfig{
					AllowHostLocalNetAddress:      allowLocalIPs,
					APIAddr:                       test.RandomLocalAddress(),
					RenterDisableIPViolationCheck: true,
					DataDir:                       antDirs[6],
					SiadPath:                      test.TestSiadFilename,
				},
				Jobs:            []string{"renter"},
				DesiredCurrency: 100000,
				Name:            test.RenterAntName,
			},
		},
		AutoConnect: true,
		WaitForSync: true,
	}
	return config
}

// DownloadAndVerifyFiles downloads given files and compares calculated
// downloaded file hashes with recorded uploaded file hashes
func DownloadAndVerifyFiles(t *testing.T, renterAnt *ant.Ant, files []ant.RenterFile) error {
	// Get renter job for downloads
	renterJob := renterAnt.Jr.NewRenterJob()
	destDir := renterAnt.Config.DataDir

	for i, f := range files {
		// Get download path
		filename := filepath.Base(f.SourceFile)
		destPath := filepath.Join(destDir, "downloadedFiles", filename)

		// Clean download path if the file exists
		err := os.Remove(destPath)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("can't delete destination file %v before download: %v", destPath, err)
		}

		// Download the file
		siaPathPath := strings.TrimLeft(f.SourceFile, "/")
		siaPath := modules.SiaPath{Path: siaPathPath}
		err = renterJob.Download(siaPath, destPath)
		if err != nil {
			return fmt.Errorf("can't download Sia file %v: %v", siaPath, err)
		}

		t.Logf("Comparing\n\tsource file: %v\n\twith downloaded file: %v\n\tusing binary: %v\n", f.SourceFile, destPath, renterAnt.Config.SiadPath)

		// Compare file hashes
		uploadedFileHash := f.MerkleRoot
		downloadedFile, err := os.Open(destPath)
		if err != nil {
			return fmt.Errorf("can't open downloaded file %v", destPath)
		}
		defer func() {
			err = errors.Compose(err, downloadedFile.Close())
		}()
		downloadedFileHash, err := ant.MerkleRoot(downloadedFile)
		if err != nil {
			return fmt.Errorf("can't get hash for downloaded file %v", destPath)
		}
		if uploadedFileHash != downloadedFileHash {
			return fmt.Errorf("file #%v uploaded file hash doesn't equal downloaded file hash", i)
		}
	}

	return nil
}
