package ant

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/persist"
	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/NebulousLabs/merkletree"

	"gitlab.com/NebulousLabs/Sia/build"
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
	uploadFileCheckFrequency = time.Second

	// uploadFileCheckLogFrequency defines how frequently to log when upload
	// gets stuck
	uploadFileCheckLogFrequency = time.Second * 20

	// renterAllowancePeriod defines the block duration of the renter's allowance
	renterAllowancePeriod = 100

	// renterDataPieces defines the number of data pieces per erasure-coded chunk
	renterDataPieces = 1

	// renterParityPieces defines the number of parity pieces per erasure-coded
	// chunk
	renterParityPieces = 4

	// renterUploadReadyTimeout defines timeout for renter to become upload
	// ready
	renterUploadReadyTimeout = time.Minute * 10

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
	downloadFileTimeout = time.Minute * 2

	// downloadFileFrequency defines how frequently the renter job checks if a
	// file is downloaded
	downloadFileCheckFrequency = time.Second

	// balanceCheckFrequency defines how frequently the renter job checks if
	// minimum treshold of coins have been mined
	balanceCheckFrequency = time.Second * 15
)

// renterPreparationPhase defines type for renter preparation phases enum
type renterPreparationPhase int

// Define possible renter phases enum
const (
	// walletFull defines a renter with filled wallet
	walletFull renterPreparationPhase = iota

	// allowanceSet defines a renter with filled wallet, default allowance set
	// and being renter upload ready
	allowanceSet

	// backgroundJobsStarted defines a renter with filled wallet, default
	// allowance set, being renter upload ready and background jobs started
	backgroundJobsStarted
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
	staticLogger *persist.Logger
	Files        []RenterFile

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
	r.staticLogger.Debugf("%v: downloading\n\tsiaFile: %v\n\tto local file: %v", r.staticJR.staticDataDir, siaPath, destPath)
	_, err = r.staticJR.staticClient.RenterDownloadGet(siaPath, destPath, 0, fileToDownload.Filesize, true, true, false)
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
		if info.Error != "" {
			msg := fmt.Sprintf("can't complete download, downloadInfo.Error: %v", info.Error)
			r.staticLogger.Errorf("%v: %v", r.staticJR.staticDataDir, msg)
			return errors.New(msg)
		}
		if hasFile && info.Completed {
			r.staticLogger.Debugf("%v: File: %v\n\tCompleted: %v\n\tReceived: %v\n\tTotalDataTransferred: %v", r.staticJR.staticDataDir, fileToDownload.SiaPath, info.Completed, info.Received, info.TotalDataTransferred)
			break
		} else if !hasFile {
			r.staticLogger.Errorf("%v: file unexpectedly missing from download list", r.staticJR.staticDataDir)
		} else {
			r.staticLogger.Debugf("%v: currently downloading %v, received %v bytes", r.staticJR.staticDataDir, fileToDownload.SiaPath, info.Received)
		}
		if time.Since(start) > downloadFileTimeout {
			msg := fmt.Sprintf("file %v hasn't been downloaded within %v timeout", siaPath, downloadFileTimeout)
			r.staticLogger.Errorf("%v: %v", r.staticJR.staticDataDir, msg)
			return errors.New(msg)
		}
	}

	// Wait for physical file become complete after download finished.
	// Sometimes after download completes the file is not completely saved to
	// disk, so there is a wait loop with timeout depending on the file size to
	// wait for physical file being completely saved to disk.
	expectedMinSavingSpeed := uint64(4e5) // bytes/second
	downloadedFileOnDiskTimeout := time.Duration(fileToDownload.Filesize/expectedMinSavingSpeed+1) * time.Second
	frequency := time.Millisecond * 100
	tries := int(downloadedFileOnDiskTimeout / frequency)
	start = time.Now()
	err = build.Retry(tries, frequency, func() error {
		fi, err := os.Stat(destPath)
		if err != nil {
			return errors.AddContext(err, "can't open destination path")
		}
		if fi.Size() != fileToDownload.Size() {
			msg := fmt.Sprintf("local file size %v doesn't match expected file size %v, waiting for file to become complete", fi.Size(), fileToDownload.Size())
			r.staticLogger.Printf("%v: %v", r.staticJR.staticDataDir, msg)
			return fmt.Errorf(msg)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("can't download complete file %v within timeout %v: %v", destPath, downloadedFileOnDiskTimeout, err)
	}

	r.staticLogger.Printf("%v: successfully downloaded\n\tsiaFile: %v\n\tto local file: %v\n\tdownload completed in: %v", r.staticJR.staticDataDir, siaPath, destPath, time.Since(start))
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
		staticLogger: j.staticLogger,
		staticJR:     j,
	}
}

// renter blocks until renter reaches the desired state defined in phase.
// Either to have a sufficiently full wallet; to set the allowance and renter
// to become upload ready; or to start periodic uploader, downloader and
// deleter jobs.
func (j *JobRunner) renter(phase renterPreparationPhase) {
	err := j.StaticTG.Add()
	if err != nil {
		j.staticLogger.Errorf("%v: can't add thread group: %v", j.staticDataDir, err)
		return
	}
	defer j.StaticTG.Done()

	// Wait for ants to be synced
	synced := j.waitForAntsSync()
	if !synced {
		j.staticLogger.Errorf("%v: waiting for ants to sync failed", j.staticDataDir)
		return
	}

	// Block until a minimum threshold of coins have been mined.
	start := time.Now()
	j.staticLogger.Debugf("%v: blocking until wallet is sufficiently full", j.staticDataDir)
	for {
		// Get the wallet balance.
		walletInfo, err := j.staticClient.WalletGet()
		if err != nil {
			// Log if there was an error.
			j.staticLogger.Errorf("%v: trouble when calling /wallet: %v", j.staticDataDir, err)
		} else if walletInfo.ConfirmedSiacoinBalance.Cmp(requiredInitialBalance) > 0 {
			// Break the wait loop when we have enough balance.
			break
		}

		// Log an error if the time elapsed has exceeded the warning threshold.
		if time.Since(start) > initialBalanceWarningTimeout {
			j.staticLogger.Errorf("%v: minimum balance for allowance has not been reached. Time elapsed: %v", j.staticDataDir, time.Since(start))
		}

		// Wait before trying to get the balance again.
		select {
		case <-j.StaticTG.StopChan():
			return
		case <-time.After(balanceCheckFrequency):
		}
	}
	j.staticLogger.Debugf("%v: wallet filled successfully.", j.staticDataDir)

	if phase == walletFull {
		return
	}

	// Block until a renter allowance has successfully been set.
	start = time.Now()
	for {
		j.staticLogger.Debugf("%v: attempting to set allowance.", j.staticDataDir)
		err := j.staticClient.RenterPostAllowance(allowance)
		j.staticLogger.Debugf("%v: allowance attempt complete", j.staticDataDir)
		if err == nil {
			// Success, we can exit the loop.
			break
		}
		// There was an error
		j.staticLogger.Errorf("%v: trouble when setting renter allowance: %v", j.staticDataDir, err)
		if time.Since(start) > setAllowanceTimeout {
			// Timeout was reached
			j.staticLogger.Errorf("%v: couldn't set allowance within %v timeout", j.staticDataDir, setAllowanceTimeout)
		}

		// Wait a bit before trying again.
		select {
		case <-j.StaticTG.StopChan():
			return
		case <-time.After(setAllowanceFrequency):
		}
	}
	j.staticLogger.Debugf("%v: renter allowance has been set successfully.", j.staticDataDir)

	err = j.WaitForRenterUploadReady()
	if err != nil {
		return
	}

	if phase == allowanceSet {
		return
	}

	// Start basic renter
	rj := j.NewRenterJob()

	// Spawn the uploader, downloader and deleter threads.
	go rj.threadedUploader()
	go rj.threadedDownloader()
	go rj.threadedDeleter()
}

// WaitForRenterUploadReady waits for renter upload ready with default timeout,
// data pieces and parity pieces if the ant has renter job. If the ant doesn't
// have renter job, it returns an error.
func (j *JobRunner) WaitForRenterUploadReady() error {
	if !j.staticAnt.HasRenterTypeJob() {
		return errors.New("this ant hasn't renter job")
	}
	// Block until renter is upload ready or till timeout is reached
	start := time.Now()
	j.staticLogger.Debugf("%v: waiting for renter to become upload ready.", j.staticDataDir)
	for {
		// Timeout
		if time.Since(start) > renterUploadReadyTimeout {
			j.staticLogger.Errorf("%v: renter is not upload ready within %v timeout.", j.staticDataDir, renterUploadReadyTimeout)
		}

		rur, err := j.staticClient.RenterUploadReadyGet(renterDataPieces, renterParityPieces)
		if err != nil {
			// Error getting RenterUploadReady
			j.staticLogger.Errorf("%v: can't get renter upload ready status: %v", j.staticDataDir, err)
		} else if rur.Ready {
			// Success, we can exit the loop.
			break
		}

		// Wait a bit before trying again.
		select {
		case <-j.StaticTG.StopChan():
			return errors.New("ant was stopped")
		case <-time.After(renterUploadReadyFrequency):
		}
	}
	j.staticLogger.Printf("%v: renter is upload ready.", j.staticDataDir)
	return nil
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

	r.staticLogger.Printf("%v: successfully deleted file.\n", r.staticJR.staticDataDir)
	err = os.Remove(r.Files[randindex].SourceFile)
	if err != nil {
		return errors.AddContext(err, "can't delete a source file")
	}
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
	err = os.Remove(destPath)
	if err != nil {
		return errors.AddContext(err, "can't delete a destination file")
	}

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
	r.staticLogger.Debugf("%v: file upload preparation beginning.", r.staticJR.staticDataDir)
	tempSubDir := filepath.Join(r.staticJR.staticDataDir, "renterSourceFiles")
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

	// Upload the file to network
	r.staticLogger.Debugf("%v: beginning file upload.", r.staticJR.staticDataDir)
	err = r.staticJR.staticClient.RenterUploadPost(sourcePath, siaPath, renterDataPieces, renterParityPieces)
	if err != nil {
		return modules.SiaPath{}, errors.AddContext(err, "error uploading a file to network")
	}
	r.staticLogger.Debugf("%v: /renter/upload call completed successfully.  Waiting for the upload to complete", r.staticJR.staticDataDir)

	// Block until the upload has reached 100%
	start := time.Now()
	var lastUploadProgress float64
	// Set lastUploadProgressLogTimestamp to past so the upload progress check
	// is able to log immediately
	lastUploadProgressLogTimestamp := time.Now().Add(-uploadFileCheckLogFrequency)
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
		r.staticLogger.Debugf("%v: upload progress: %v%%", r.staticJR.staticDataDir, uploadProgress)
		if uploadProgress == 100 {
			// The file has finished uploading
			break
		}

		// If there is no progress in the upload log number of active hosts and
		// contracts. Repeat the contracts number log every according to the
		// given frequency.
		if uploadProgress == lastUploadProgress && time.Since(lastUploadProgressLogTimestamp) > uploadFileCheckLogFrequency {
			lastUploadProgressLogTimestamp = time.Now()
			// Log number of hostdb active hosts
			hdag, err := r.staticJR.staticClient.HostDbActiveGet()
			if err != nil {
				r.staticLogger.Errorf("%v: can't get hostdb active hosts: %v", r.staticJR.staticDataDir, err)
			} else {
				r.staticLogger.Debugf("%v: number of HostDB Active Hosts: %v", r.staticJR.staticDataDir, len(hdag.Hosts))
			}

			// Log number of each type of contract
			rc, err := r.staticJR.staticClient.RenterAllContractsGet()
			if err != nil {
				r.staticLogger.Errorf("%v: can't get renter contracts: %v", r.staticJR.staticDataDir, err)
			} else {
				var msg string
				msg += fmt.Sprintf("%v: number of Contracts: %v\n", r.staticJR.staticDataDir, len(rc.Contracts))
				msg += fmt.Sprintf("%v: number of ActiveContracts: %v\n", r.staticJR.staticDataDir, len(rc.ActiveContracts))
				msg += fmt.Sprintf("%v: number of DisabledContracts: %v\n", r.staticJR.staticDataDir, len(rc.DisabledContracts))
				msg += fmt.Sprintf("%v: number of ExpiredContracts: %v\n", r.staticJR.staticDataDir, len(rc.ExpiredContracts))
				msg += fmt.Sprintf("%v: number of ExpiredRefreshedContracts: %v\n", r.staticJR.staticDataDir, len(rc.ExpiredRefreshedContracts))
				msg += fmt.Sprintf("%v: number of InactiveContracts: %v\n", r.staticJR.staticDataDir, len(rc.InactiveContracts))
				msg += fmt.Sprintf("%v: number of PassiveContracts: %v\n", r.staticJR.staticDataDir, len(rc.PassiveContracts))
				msg += fmt.Sprintf("%v: number of RecoverableContracts: %v\n", r.staticJR.staticDataDir, len(rc.RecoverableContracts))
				msg += fmt.Sprintf("%v: number of RefreshedContracts: %v\n", r.staticJR.staticDataDir, len(rc.RefreshedContracts))
				r.staticLogger.Debugln(msg)
			}
		}
		lastUploadProgress = uploadProgress

		// Check timeout
		if time.Since(start) > uploadTimeout {
			// Log error
			err := fmt.Errorf("file with siaPath %v could not be fully uploaded within %v timeout. Progress reached: %v%%", siaPath, uploadTimeout, uploadProgress)
			r.staticLogger.Errorf("%v: %v", r.staticJR.staticDataDir, err)
			return modules.SiaPath{}, err
		}
	}
	r.staticLogger.Printf("%v: file has been successfully uploaded to 100%%.", r.staticJR.staticDataDir)
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
			r.staticLogger.Errorf("%v: can't delete random file: %v", r.staticJR.staticDataDir, err)
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
			r.staticLogger.Errorf("%v: can't download random file: %v", r.staticJR.staticDataDir, err)
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
	err = os.Mkdir(filepath.Join(r.staticJR.staticDataDir, "renterSourceFiles"), 0700)
	if err != nil {
		return
	}

	for {
		// Wait a while between upload attempts.
		select {
		case <-r.staticJR.StaticTG.StopChan():
			return
		case <-time.After(uploadFileFrequency):
		}

		// Upload a file.
		if _, err := r.managedUpload(uploadFileSize); err != nil {
			r.staticLogger.Errorf("%v: can't upload file: %v", r.staticJR.staticDataDir, err)
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
