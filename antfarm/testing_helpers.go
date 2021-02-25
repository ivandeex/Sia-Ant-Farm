package antfarm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/ant"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/persist"
	"gitlab.com/NebulousLabs/Sia-Ant-Farm/test"
	"gitlab.com/NebulousLabs/Sia/modules"
	"gitlab.com/NebulousLabs/errors"
)

// NewDefaultRenterAntfarmTestingConfig creates default basic antfarm config
// for running renter tests
func NewDefaultRenterAntfarmTestingConfig(dataDir string, allowLocalIPs bool) (AntfarmConfig, error) {
	antFarmAddr, err := test.RandomFreeLocalAddress()
	if err != nil {
		return AntfarmConfig{}, errors.AddContext(err, "can't create antfarm local address")
	}
	antFarmDir := filepath.Join(dataDir, "antfarm-data")
	antDirs, err := test.AntDirs(dataDir, 7)
	if err != nil {
		return AntfarmConfig{}, errors.AddContext(err, "can't create ant data directories")
	}
	addrs, err := test.RandomFreeLocalAddresses(7)
	if err != nil {
		return AntfarmConfig{}, errors.AddContext(err, "can't create ant local addresses")
	}
	config := AntfarmConfig{
		ListenAddress: antFarmAddr,
		DataDir:       antFarmDir,
		AntConfigs: []ant.AntConfig{
			{
				SiadConfig: ant.SiadConfig{
					AllowHostLocalNetAddress: allowLocalIPs,
					APIAddr:                  addrs[0],
					DataDir:                  antDirs[0],
					SiadPath:                 test.TestSiadFilename,
				},
				Jobs: []string{"gateway", "miner"},
			},
			{
				SiadConfig: ant.SiadConfig{
					AllowHostLocalNetAddress: allowLocalIPs,
					APIAddr:                  addrs[1],
					DataDir:                  antDirs[1],
					SiadPath:                 test.TestSiadFilename,
				},
				Jobs:            []string{"host"},
				DesiredCurrency: 100000,
			},
			{
				SiadConfig: ant.SiadConfig{
					AllowHostLocalNetAddress: allowLocalIPs,
					APIAddr:                  addrs[2],
					DataDir:                  antDirs[2],
					SiadPath:                 test.TestSiadFilename,
				},
				Jobs:            []string{"host"},
				DesiredCurrency: 100000,
			},
			{
				SiadConfig: ant.SiadConfig{
					AllowHostLocalNetAddress: allowLocalIPs,
					APIAddr:                  addrs[3],
					DataDir:                  antDirs[3],
					SiadPath:                 test.TestSiadFilename,
				},
				Jobs:            []string{"host"},
				DesiredCurrency: 100000,
			},
			{
				SiadConfig: ant.SiadConfig{
					AllowHostLocalNetAddress: allowLocalIPs,
					APIAddr:                  addrs[4],
					DataDir:                  antDirs[4],
					SiadPath:                 test.TestSiadFilename,
				},
				Jobs:            []string{"host"},
				DesiredCurrency: 100000,
			},
			{
				SiadConfig: ant.SiadConfig{
					AllowHostLocalNetAddress: allowLocalIPs,
					APIAddr:                  addrs[5],
					DataDir:                  antDirs[5],
					SiadPath:                 test.TestSiadFilename,
				},
				Jobs:            []string{"host"},
				DesiredCurrency: 100000,
			},
			{
				SiadConfig: ant.SiadConfig{
					AllowHostLocalNetAddress:      allowLocalIPs,
					APIAddr:                       addrs[6],
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
	return config, nil
}

// NewAntfarmConfig creates a new antfarm config. Ants of different types have
// standardized names.
func NewAntfarmConfig(dataDir string, allowLocalIPs bool, miners int, hosts int, renters int, generic int) (AntfarmConfig, error) {
	antFarmAddr, err := test.RandomFreeLocalAddress()
	if err != nil {
		return AntfarmConfig{}, errors.AddContext(err, "can't create antfarm local addresses")
	}
	antFarmDir := filepath.Join(dataDir, "antfarm-data")
	antCount := miners + hosts + renters + generic
	antAddrs, err := test.RandomFreeLocalAddresses(antCount)
	if err != nil {
		return AntfarmConfig{}, errors.AddContext(err, "can't create ant local addresses")
	}
	antDirs, err := test.AntDirs(dataDir, antCount)
	if err != nil {
		return AntfarmConfig{}, errors.AddContext(err, "can't create ant data directories")
	}

	// Prepare ant configs
	antConfigs := []ant.AntConfig{}
	var doneAntConfigs int

	// Add miners
	for i := 0; i < miners; i++ {
		ac := ant.AntConfig{
			SiadConfig: ant.SiadConfig{
				AllowHostLocalNetAddress: allowLocalIPs,
				APIAddr:                  antAddrs[doneAntConfigs],
				DataDir:                  antDirs[doneAntConfigs],
				SiadPath:                 test.TestSiadFilename,
			},
			Jobs: []string{"gateway", "miner"},
			Name: ant.NameMiner(i),
		}
		antConfigs = append(antConfigs, ac)
		doneAntConfigs++
	}

	// Add hosts
	for i := 0; i < hosts; i++ {
		ac := ant.AntConfig{
			SiadConfig: ant.SiadConfig{
				AllowHostLocalNetAddress: allowLocalIPs,
				APIAddr:                  antAddrs[doneAntConfigs],
				DataDir:                  antDirs[doneAntConfigs],
				SiadPath:                 test.TestSiadFilename,
			},
			Jobs:            []string{"host"},
			Name:            ant.NameHost(i),
			DesiredCurrency: 100000,
		}
		antConfigs = append(antConfigs, ac)
		doneAntConfigs++
	}

	// Add renters
	for i := 0; i < renters; i++ {
		ac := ant.AntConfig{
			SiadConfig: ant.SiadConfig{
				AllowHostLocalNetAddress:      allowLocalIPs,
				APIAddr:                       antAddrs[doneAntConfigs],
				RenterDisableIPViolationCheck: true,
				DataDir:                       antDirs[doneAntConfigs],
				SiadPath:                      test.TestSiadFilename,
			},
			Jobs:            []string{"renter"},
			Name:            ant.NameRenter(i),
			DesiredCurrency: 100000,
		}
		antConfigs = append(antConfigs, ac)
		doneAntConfigs++
	}

	// Add generic ants
	for i := 0; i < generic; i++ {
		ac := ant.AntConfig{
			SiadConfig: ant.SiadConfig{
				AllowHostLocalNetAddress: allowLocalIPs,
				APIAddr:                  antAddrs[doneAntConfigs],
				DataDir:                  antDirs[doneAntConfigs],
				SiadPath:                 test.TestSiadFilename,
			},
			Jobs: []string{"generic"},
			Name: ant.NameGeneric(i),
		}
		antConfigs = append(antConfigs, ac)
		doneAntConfigs++
	}

	config := AntfarmConfig{
		ListenAddress: antFarmAddr,
		DataDir:       antFarmDir,
		AntConfigs:    antConfigs,
		AutoConnect:   true,
		WaitForSync:   true,
	}
	return config, nil
}

// DownloadAndVerifyFiles downloads given files and compares calculated
// downloaded file hashes with recorded uploaded file hashes
func DownloadAndVerifyFiles(logger *persist.Logger, renterAnt *ant.Ant, files []ant.RenterFile) error {
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

		logger.Printf("Comparing\n\tsource file: %v\n\twith downloaded file: %v\n\tusing binary: %v\n", f.SourceFile, destPath, renterAnt.Config.SiadPath)

		// Compare file hashes
		sourceFileHash := f.MerkleRoot
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
		if sourceFileHash != downloadedFileHash {
			dfi, err := os.Stat(destPath)
			if err != nil {
				return errors.AddContext(err, "can't get downloaded file info")
			}
			sfi, err := os.Stat(f.SourceFile)
			if err != nil {
				return errors.AddContext(err, "can't get source file info")
			}
			err = fmt.Errorf("file #%v downloaded file hash doesn't equal source file hash", i)
			err = fmt.Errorf("%v\nfile #%v downloaded file length: %d, source file length: %d", err, i, dfi.Size(), sfi.Size())
			logger.Errorln(err)
			return err
		}
	}

	return nil
}
