package ant

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/NebulousLabs/merkletree"

	"gitlab.com/NebulousLabs/Sia/crypto"
	"gitlab.com/NebulousLabs/Sia/modules"
	"gitlab.com/NebulousLabs/Sia/node/api"
	"gitlab.com/NebulousLabs/Sia/node/api/client"
	"gitlab.com/NebulousLabs/Sia/types"
	"gitlab.com/NebulousLabs/fastrand"
)

const (
	// downloadFileFrequency defines how frequently the renter job downloads
	// files from the network.
	downloadFileFrequency = uploadFileFrequency * 3 / 2

	// initialBalanceWarningTimeout defines how long the renter will wait before
	// reporting to the user that the required initial balance has not been
	// reached.
	initialBalanceWarningTimeout = time.Minute * 10

	// setAllowanceTimeout defines how long the renter will wait before
	// reporting to the user that the allowance has not yet been set
	// successfully.
	setAllowanceTimeout = time.Minute * 2

	// setAllowanceFrequency defines how frequently the renter job tries to set
	// renter allowance
	setAllowanceFrequency = time.Second * 15

	// uploadFileFrequency defines how frequently the renter job uploads files
	// to the network.
	uploadFileFrequency = time.Second * 60

	// deleteFileFrequency defines how frequently the renter job deletes files
	// from the network.
	deleteFileFrequency = time.Minute * 2

	// deleteFileThreshold defines the minimum number of files uploaded before
	// deletion occurs.
	deleteFileThreshold = 30

	// uploadTimeout defines the maximum time allowed for an upload operation to
	// complete, ie for an upload to reach 100%.
	uploadTimeout = time.Minute * 10

	// uploadFileCheckFrequency defines how frequently the renter job checks if
	// file upload has reached 100%
	uploadFileCheckFrequency = time.Second * 20

	// renterAllowancePeriod defines the block duration of the renter's allowance
	renterAllowancePeriod = 100

	// renterDataPieces defines the number of data pieces per erasure-coded chunk
	renterDataPieces = 1

	// renterParityPieces defines the number of parity pieces per erasure-coded
	// chunk
	renterParityPieces = 4

	// renterUploadReadyTimeout defines timeout for renter to become upload
	// ready
	renterUploadReadyTimeout = time.Minute * 5

	// renterUploadReadyFrequency defines how frequently the renter job checks
	// if renter became upload ready
	renterUploadReadyFrequency = time.Second * 5

	// uploadFileSize defines the size of the test files to be uploaded.  Test
	// files are filled with random data.
	uploadFileSize = 1e8

	// fileAppearInDownloadListTimeout defines timeout of a file to appear in the
	// download list
	fileAppearInDownloadListTimeout = time.Minute * 3

	// fileApearInDownloadListFrequency defines how frequently the renter job
	// checks if a file appears in the download list
	fileApearInDownloadListFrequency = time.Second

	// downloadFileTimeout defines timeout for file to be downloaded
	downloadFileTimeout = time.Minute * 5

	// downloadFileFrequency defines how frequently the renter job checks if a
	// file is downloaded
	downloadFileCheckFrequency = time.Second

	// balanceCheckFrequency defines how frequently the renter job checks if
	// minimum treshold of coins have been mined
	balanceCheckFrequency = time.Second * 15
)

var (
	// allowance is the set of allowance settings that will be used by
	// renter
	allowance = modules.Allowance{
		Funds:       types.NewCurrency64(20e3).Mul(types.SiacoinPrecision),
		Hosts:       5,
		Period:      renterAllowancePeriod,
		RenewWindow: renterAllowancePeriod / 4,

		ExpectedStorage:    10e9,
		ExpectedUpload:     2e9 / renterAllowancePeriod,
		ExpectedDownload:   1e12 / renterAllowancePeriod,
		ExpectedRedundancy: 3.0,
		MaxPeriodChurn:     2.5e9,
	}

	// requiredInitialBalance sets the number of coins that the renter requires
	// before uploading will begin.
	requiredInitialBalance = types.NewCurrency64(100e3).Mul(types.SiacoinPrecision)
)

// RenterFile stores the location and checksum of a file active on the renter.
type RenterFile struct {
	MerkleRoot crypto.Hash
	SourceFile string
}

// RenterJob contains statefulness that is used to drive the renter. Most
// importantly, it contains a list of files that the renter is currently
// uploading to the network.
type RenterJob struct {
	Files []RenterFile

	staticJR *JobRunner
	mu       sync.Mutex
}

// createTempFile creates temporary file in the given temporary sub-directory,
// with the given filename pattern. The file is filled with random data of the
// given length.
func createTempFile(dir, fileNamePattern string, fileSize uint64) (absFilePath string, merkleRoot crypto.Hash, err error) {
	err = os.MkdirAll(dir, 0700)
	if err != nil {
		return "", crypto.Hash{}, fmt.Errorf("error creating an upload directory: %v", err)
	}
	f, err := ioutil.TempFile(dir, fileNamePattern)
	if err != nil {
		return "", crypto.Hash{}, fmt.Errorf("error creating temp file: %v", err)
	}
	defer func() {
		err = errors.Compose(err, f.Close())
	}()

	absFilePath, err = filepath.Abs(f.Name())
	if err != nil {
		return "", crypto.Hash{}, fmt.Errorf("error getting temp file absolute path: %v", err)
	}

	// Fill the file with random data.
	merkleRoot, err = randFillFile(f, fileSize)
	if err != nil {
		return "", crypto.Hash{}, fmt.Errorf("error filling file with random data: %v", err)
	}

	return absFilePath, merkleRoot, nil
}

// downloadFile is a helper function to download the given file from the
// network to the given path.
func downloadFile(r *RenterJob, fileToDownload modules.FileInfo, destPath string) error {
	siaPath := fileToDownload.SiaPath
	destPath, err := filepath.Abs(destPath)
	if err != nil {
		return fmt.Errorf("error getting absolute path from %v: %v", destPath, err)
	}
	log.Printf("[INFO] [renter] [%v] Downloading\n\tsiaFile: %v\n\tto local file: %v\n", r.staticJR.staticSiaDirectory, siaPath, destPath)
	_, err = r.staticJR.staticClient.RenterDownloadGet(siaPath, destPath, 0, fileToDownload.Filesize, true, true)
	if err != nil {
		return errors.AddContext(err, "failed in call to /renter/download")
	}

	// Wait for the file to appear in the download queue
	start := time.Now()
	for {
		select {
		case <-r.staticJR.StaticTG.StopChan():
			return nil
		case <-time.After(fileApearInDownloadListFrequency):
		}

		hasFile, _, err := isFileInDownloads(r.staticJR.staticClient, fileToDownload)
		if err != nil {
			return errors.AddContext(err, "error checking renter download queue")
		}
		if hasFile {
			break
		}
		if time.Since(start) > fileAppearInDownloadListTimeout {
			return fmt.Errorf("file %v hasn't apear in renter download list within %v timeout", siaPath, fileAppearInDownloadListTimeout)
		}
	}

	// Wait for the file to be finished downloading with a timeout
	start = time.Now()
	for {
		select {
		case <-r.staticJR.StaticTG.StopChan():
			return nil
		case <-time.After(downloadFileCheckFrequency):
		}

		hasFile, info, err := isFileInDownloads(r.staticJR.staticClient, fileToDownload)
		if err != nil {
			return errors.AddContext(err, "error checking renter download queue")
		}
		if hasFile && info.Received == info.Filesize {
			break
		} else if !hasFile {
			log.Printf("[INFO] [renter] [%v] File unexpectedly missing from download list\n", r.staticJR.staticSiaDirectory)
		} else {
			log.Printf("[INFO] [renter] [%v] Currently downloading %v, received %v bytes\n", r.staticJR.staticSiaDirectory, fileToDownload.SiaPath, info.Received)
		}
		if time.Since(start) > downloadFileTimeout {
			log.Printf("[ERROR] [renter] [%v] File %v hasn't been downloaded within %v timeout\n", r.staticJR.staticSiaDirectory, siaPath, downloadFileTimeout)
			return fmt.Errorf("file %v hasn't been downloaded within %v timeout", siaPath, downloadFileTimeout)
		}
	}
	log.Printf("[INFO] [renter] [%v] Successfully downloaded\n\tsiaFile: %v\n\tto local file:%v\n", r.staticJR.staticSiaDirectory, siaPath, destPath)
	return nil
}

// isFileInDownloads grabs the files currently being downloaded by the
// renter and returns bool `true` if fileToDownload exists in the
// download list.  It also returns the DownloadInfo for the requested `file`.
func isFileInDownloads(client *client.Client, file modules.FileInfo) (bool, api.DownloadInfo, error) {
	var dlinfo api.DownloadInfo
	renterDownloads, err := client.RenterDownloadsGet()
	if err != nil {
		return false, dlinfo, err
	}

	hasFile := false
	for _, download := range renterDownloads.Downloads {
		if download.SiaPath == file.SiaPath {
			hasFile = true
			dlinfo = download
		}
	}

	return hasFile, dlinfo, nil
}

// MerkleRoot calculates merkle root of the file given in reader
func MerkleRoot(r io.Reader) (h crypto.Hash, err error) {
	root, err := merkletree.ReaderRoot(r, crypto.NewHash(), crypto.SegmentSize)
	copy(h[:], root)
	return
}

// randFillFile will append 'size' bytes to the input file, returning the
// merkle root of the bytes that were appended.
func randFillFile(f *os.File, size uint64) (h crypto.Hash, err error) {
	tee := io.TeeReader(io.LimitReader(fastrand.Reader, int64(size)), f)
	h, err = MerkleRoot(tee)
	return
}

// NewRenterJob returns new renter job
func (j *JobRunner) NewRenterJob() RenterJob {
	return RenterJob{
		staticJR: j,
	}
}

// renter blocks until renter has a sufficiently full wallet, the allowance is
// set, and until renter is upload ready. Then it optionally starts periodic
// uploader, downloader and deleter jobs.
func (j *JobRunner) renter(startBackgroundJobs bool) {
	err := j.StaticTG.Add()
	if err != nil {
		return
	}
	defer j.StaticTG.Done()

	// Wait for ants to be synced
	synced := j.waitForAntsSync()
	if !synced {
		return
	}

	// Start basic renter
	rj := j.NewRenterJob()

	// Block until a minimum threshold of coins have been mined.
	start := time.Now()
	log.Printf("[INFO] [renter] [%v] Blocking until wallet is sufficiently full\n", rj.staticJR.staticSiaDirectory)
	for {
		// Get the wallet balance.
		walletInfo, err := rj.staticJR.staticClient.WalletGet()
		if err != nil {
			// Log if there was an error.
			log.Printf("[ERROR] [renter] [%v] Trouble when calling /wallet: %v\n", rj.staticJR.staticSiaDirectory, err)
		} else if walletInfo.ConfirmedSiacoinBalance.Cmp(requiredInitialBalance) > 0 {
			// Break the wait loop when we have enough balance.
			break
		}

		// Log an error if the time elapsed has exceeded the warning threshold.
		if time.Since(start) > initialBalanceWarningTimeout {
			log.Printf("[ERROR] [renter] [%v] Minimum balance for allowance has not been reached. Time elapsed: %v\n", rj.staticJR.staticSiaDirectory, time.Since(start))
		}

		// Wait before trying to get the balance again.
		select {
		case <-rj.staticJR.StaticTG.StopChan():
			return
		case <-time.After(balanceCheckFrequency):
		}
	}
	log.Printf("[INFO] [renter] [%v] Wallet filled successfully. Blocking until allowance has been set.\n", rj.staticJR.staticSiaDirectory)

	// Block until a renter allowance has successfully been set.
	start = time.Now()
	for {
		log.Printf("[DEBUG] [renter] [%v] Attempting to set allowance.\n", rj.staticJR.staticSiaDirectory)
		err := rj.staticJR.staticClient.RenterPostAllowance(allowance)
		log.Printf("[DEBUG] [renter] [%v] Allowance attempt complete: %v\n", rj.staticJR.staticSiaDirectory, err)
		if err == nil {
			// Success, we can exit the loop.
			break
		}
		// There was an error
		log.Printf("[ERROR] [renter] [%v] Trouble when setting renter allowance: %v\n", rj.staticJR.staticSiaDirectory, err)
		if time.Since(start) > setAllowanceTimeout {
			// Timeout was reached
			log.Printf("[ERROR] [renter] [%v] Couldn't set allowance within %v timeout\n", rj.staticJR.staticSiaDirectory, setAllowanceTimeout)
		}

		// Wait a bit before trying again.
		select {
		case <-rj.staticJR.StaticTG.StopChan():
			return
		case <-time.After(setAllowanceFrequency):
		}
	}
	log.Printf("[INFO] [renter] [%v] Renter allowance has been set successfully.\n", rj.staticJR.staticSiaDirectory)

	// Block until renter is upload ready
	start = time.Now()
	log.Printf("[INFO] [renter] [%v] Waiting for renter to become upload ready.\n", rj.staticJR.staticSiaDirectory)
	for {
		rur, err := rj.staticJR.staticClient.RenterUploadReadyGet(renterDataPieces, renterParityPieces)
		if err != nil {
			// Error getting RenterUploadReady
			log.Printf("[ERROR] [renter] [%v] Trouble when getting renter upload ready status: %v\n", rj.staticJR.staticSiaDirectory, err)
		} else if rur.Ready {
			// Success, we can exit the loop.
			break
		}
		if time.Since(start) > renterUploadReadyTimeout {
			// We have hit the timeout
			log.Printf("[ERROR] [renter] [%v] Renter is not upload ready within %v timeout.\n", rj.staticJR.staticSiaDirectory, renterUploadReadyTimeout)
		}

		// Wait a bit before trying again.
		select {
		case <-rj.staticJR.StaticTG.StopChan():
			return
		case <-time.After(renterUploadReadyFrequency):
		}
	}
	log.Printf("[INFO] [renter] [%v] Renter is upload ready.\n", rj.staticJR.staticSiaDirectory)

	if startBackgroundJobs {
		// Spawn the uploader, downloader and deleter threads.
		go rj.threadedUploader()
		go rj.threadedDownloader()
		go rj.threadedDeleter()
	}
}

// Download will download the given file from the network to the given
// destination path.
func (r *RenterJob) Download(siaPath modules.SiaPath, destPath string) error {
	err := r.staticJR.StaticTG.Add()
	if err != nil {
		return errors.AddContext(err, "can't download a file")
	}
	defer r.staticJR.StaticTG.Done()

	return r.managedDownload(siaPath, destPath)
}

// managedDeleteRandom deletes a random file from the renter.
func (r *RenterJob) managedDeleteRandom() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// no-op with fewer than 10 files
	if len(r.Files) < deleteFileThreshold {
		return nil
	}

	randindex := fastrand.Intn(len(r.Files))

	path, err := modules.NewSiaPath(r.Files[randindex].SourceFile)
	if err != nil {
		return err
	}
	if err := r.staticJR.staticClient.RenterFileDeletePost(path); err != nil {
		return err
	}

	log.Printf("[%v jobStorageRenter INFO]: successfully deleted file\n", r.staticJR.staticSiaDirectory)
	os.Remove(r.Files[randindex].SourceFile)
	r.Files = append(r.Files[:randindex], r.Files[randindex+1:]...)

	return nil
}

// Download will managed download the given file from the network to the given
// destination path.
func (r *RenterJob) managedDownload(siaPath modules.SiaPath, destPath string) error {
	// Check file is in renter file list and is available
	renterFiles, err := r.staticJR.staticClient.RenterFilesGet(false) // cached=false
	if err != nil {
		return errors.AddContext(err, "error calling /renter/files")
	}
	var fileToDownload modules.FileInfo
	for _, file := range renterFiles.Files {
		if file.SiaPath == siaPath {
			fileToDownload = file
			break
		}
	}
	if fileToDownload.SiaPath != siaPath {
		return fmt.Errorf("file %v is not in renter file list", siaPath)
	}
	if !fileToDownload.Available {
		return fmt.Errorf("file %v is in renter file list, but is not available to download", siaPath)
	}

	// Delete file if it exists
	err = os.Remove(destPath)
	if err != nil && !os.IsNotExist(err) {
		return errors.AddContext(err, "error deleting destination file")
	}

	// Create destination directory if it doesn't exist
	destDir := filepath.Dir(destPath)
	err = os.MkdirAll(destDir, 0700)
	if err != nil {
		return errors.AddContext(err, "error creating destination directory")
	}

	// Download the file
	err = downloadFile(r, fileToDownload, destPath)
	if err != nil {
		return errors.AddContext(err, "failed to download the file")
	}

	return nil
}

// managedDownloadRandomFile will managed download a random file from the network.
func (r *RenterJob) managedDownloadRandomFile() error {
	// Download a random file from the renter's file list
	renterFiles, err := r.staticJR.staticClient.RenterFilesGet(false) // cached=false
	if err != nil {
		return fmt.Errorf("error calling /renter/files: %v", err)
	}

	// Filter out files which are not available.
	availableFiles := renterFiles.Files[:0]
	for _, file := range renterFiles.Files {
		if file.Available {
			availableFiles = append(availableFiles, file)
		}
	}

	// Do nothing if there are not any files to be downloaded.
	if len(availableFiles) == 0 {
		return fmt.Errorf("tried to download a file, but none were available")
	}

	// Download a file at random.
	fileToDownload := availableFiles[fastrand.Intn(len(availableFiles))]

	// Use ioutil.TempFile to get a random temporary filename.
	f, err := ioutil.TempFile("", "antfarm-renter")
	if err != nil {
		return fmt.Errorf("failed to create temporary file for download: %v", err)
	}
	defer func() {
		err = errors.Compose(err, f.Close())
	}()
	destPath, _ := filepath.Abs(f.Name())
	os.Remove(destPath)

	// Download the file
	err = downloadFile(r, fileToDownload, destPath)
	if err != nil {
		return errors.AddContext(err, "failed to download the file")
	}

	return nil
}

// managedUpload will managed upload a file with given size to the network.
func (r *RenterJob) managedUpload(fileSize uint64) (siaPath modules.SiaPath, err error) {
	// Generate some random data to upload. The file needs to be closed before
	// the upload to the network starts.
	log.Printf("[INFO] [renter] [%v] File upload preparation beginning.\n", r.staticJR.staticSiaDirectory)
	tempSubDir := filepath.Join(r.staticJR.staticSiaDirectory, "renterSourceFiles")
	pattern := "renterFile"
	sourcePath, merkleRoot, err := createTempFile(tempSubDir, pattern, fileSize)
	if err != nil {
		return modules.SiaPath{}, errors.AddContext(err, "error creating file to upload")
	}

	// Get Sia path
	siaPath, err = modules.NewSiaPath(sourcePath)
	if err != nil {
		return modules.SiaPath{}, errors.AddContext(err, "error creating SiaPath")
	}

	// Add the file to the renter.
	rf := RenterFile{
		MerkleRoot: merkleRoot,
		SourceFile: sourcePath,
	}
	r.mu.Lock()
	r.Files = append(r.Files, rf)
	r.mu.Unlock()
	log.Printf("[INFO] [renter] [%v] File upload preparation complete, beginning file upload.\n", r.staticJR.staticSiaDirectory)

	// Upload the file to network
	log.Printf("[INFO] [renter] [%v] Beginning file upload.\n", r.staticJR.staticSiaDirectory)
	err = r.staticJR.staticClient.RenterUploadPost(sourcePath, siaPath, renterDataPieces, renterParityPieces)
	if err != nil {
		return modules.SiaPath{}, errors.AddContext(err, "error uploading a file to network")
	}
	log.Printf("[INFO] [renter] [%v] /renter/upload call completed successfully.  Waiting for the upload to complete\n", r.staticJR.staticSiaDirectory)

	// Block until the upload has reached 100%
	start := time.Now()
	var lastUploadProgress float64
	for {
		select {
		case <-r.staticJR.StaticTG.StopChan():
			return modules.SiaPath{}, nil
		case <-time.After(uploadFileCheckFrequency):
		}

		rfg, err := r.staticJR.staticClient.RenterFilesGet(false) // cached=false
		if err != nil {
			return modules.SiaPath{}, errors.AddContext(err, "error calling /renter/files")
		}

		// Check upload progress
		var uploadProgress float64
		for _, file := range rfg.Files {
			if file.SiaPath == siaPath {
				uploadProgress = file.UploadProgress
				break
			}
		}
		log.Printf("[INFO] [renter] [%v] upload progress: %v%%\n", r.staticJR.staticSiaDirectory, uploadProgress)
		if uploadProgress == 100 {
			// The file has finished uploading
			break
		}

		// If there is no progress in the upload log number of active hosts and
		// contracts
		if uploadProgress == lastUploadProgress {
			// Log number of hostdb active hosts
			hdag, err := r.staticJR.staticClient.HostDbActiveGet()
			if err != nil {
				log.Printf("[ERROR] [renter] [%v] Can't get hostdb active hosts: %v\n", r.staticJR.staticSiaDirectory, err)
			} else {
				log.Printf("[DEBUG] [renter] [%v] Number of HostDB Active Hosts: %v\n", r.staticJR.staticSiaDirectory, len(hdag.Hosts))
			}

			// Log number of each type of contract
			rc, err := r.staticJR.staticClient.RenterAllContractsGet()
			if err != nil {
				log.Printf("[ERROR] [renter] [%v] Can't get renter contracts: %v\n", r.staticJR.staticSiaDirectory, err)
			} else {
				log.Printf("[DEBUG] [renter] [%v] Number of Contracts: %v\n", r.staticJR.staticSiaDirectory, len(rc.Contracts))
				log.Printf("[DEBUG] [renter] [%v] Number of ActiveContracts: %v\n", r.staticJR.staticSiaDirectory, len(rc.ActiveContracts))
				log.Printf("[DEBUG] [renter] [%v] Number of DisabledContracts: %v\n", r.staticJR.staticSiaDirectory, len(rc.DisabledContracts))
				log.Printf("[DEBUG] [renter] [%v] Number of ExpiredContracts: %v\n", r.staticJR.staticSiaDirectory, len(rc.ExpiredContracts))
				log.Printf("[DEBUG] [renter] [%v] Number of ExpiredRefreshedContracts: %v\n", r.staticJR.staticSiaDirectory, len(rc.ExpiredRefreshedContracts))
				log.Printf("[DEBUG] [renter] [%v] Number of InactiveContracts: %v\n", r.staticJR.staticSiaDirectory, len(rc.InactiveContracts))
				log.Printf("[DEBUG] [renter] [%v] Number of PassiveContracts: %v\n", r.staticJR.staticSiaDirectory, len(rc.PassiveContracts))
				log.Printf("[DEBUG] [renter] [%v] Number of RecoverableContracts: %v\n", r.staticJR.staticSiaDirectory, len(rc.RecoverableContracts))
				log.Printf("[DEBUG] [renter] [%v] Number of RefreshedContracts: %v\n", r.staticJR.staticSiaDirectory, len(rc.RefreshedContracts))
			}
		}
		lastUploadProgress = uploadProgress

		// Check timeout
		if time.Since(start) > uploadTimeout {
			// Log error
			msg := fmt.Sprintf("file with siaPath %v could not be fully uploaded within %v timeout. Progress reached: %v%%", siaPath, uploadTimeout, uploadProgress)
			log.Printf("[ERROR] [renter] [%v] %v", r.staticJR.staticSiaDirectory, msg)
			return modules.SiaPath{}, errors.New(msg)
		}
	}
	log.Printf("[INFO] [renter] [%v] file has been successfully uploaded to 100%%.\n", r.staticJR.staticSiaDirectory)
	return siaPath, nil
}

// threadedDeleter deletes one random file from the renter every 100 seconds
// once 10 or more files have been uploaded.
func (r *RenterJob) threadedDeleter() {
	err := r.staticJR.StaticTG.Add()
	if err != nil {
		return
	}
	defer r.staticJR.StaticTG.Done()

	for {
		select {
		case <-r.staticJR.StaticTG.StopChan():
			return
		case <-time.After(deleteFileFrequency):
		}

		if err := r.managedDeleteRandom(); err != nil {
			log.Printf("[ERROR] [renter] [%v]: %v\n", r.staticJR.staticSiaDirectory, err)
		}
	}
}

// threadedDownloader is a function that continuously runs for the renter job,
// downloading a file at random every 400 seconds.
func (r *RenterJob) threadedDownloader() {
	err := r.staticJR.StaticTG.Add()
	if err != nil {
		return
	}
	defer r.staticJR.StaticTG.Done()

	// Wait for the first file to be uploaded before starting the download
	// loop.
	for {
		select {
		case <-r.staticJR.StaticTG.StopChan():
			return
		case <-time.After(downloadFileFrequency):
		}

		// Download a file.
		if err := r.managedDownloadRandomFile(); err != nil {
			log.Printf("[ERROR] [renter] [%v]: %v\n", r.staticJR.staticSiaDirectory, err)
		}
	}
}

// threadedUploader is a function that continuously runs for the renter job,
// uploading a 500MB file every 240 seconds (10 blocks). The renter should have
// already set an allowance.
func (r *RenterJob) threadedUploader() {
	err := r.staticJR.StaticTG.Add()
	if err != nil {
		return
	}
	defer r.staticJR.StaticTG.Done()

	// Make the source files directory
	os.Mkdir(filepath.Join(r.staticJR.staticSiaDirectory, "renterSourceFiles"), 0700)
	for {
		// Wait a while between upload attempts.
		select {
		case <-r.staticJR.StaticTG.StopChan():
			return
		case <-time.After(uploadFileFrequency):
		}

		// Upload a file.
		if _, err := r.managedUpload(uploadFileSize); err != nil {
			log.Printf("[ERROR] [renter] [%v]: %v\n", r.staticJR.staticSiaDirectory, err)
		}
	}
}

// Upload will upload a file with given size to the network.
func (r *RenterJob) Upload(fileSize uint64) (siaPath modules.SiaPath, err error) {
	err = r.staticJR.StaticTG.Add()
	if err != nil {
		return modules.SiaPath{}, errors.AddContext(err, "can't upload a file")
	}
	defer r.staticJR.StaticTG.Done()

	return r.managedUpload(fileSize)
}
