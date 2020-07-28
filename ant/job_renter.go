package ant

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

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

	// InitialBalanceWarningTimeout defines how long the renter will wait before
	// reporting to the user that the required initial balance has not been
	// reached.
	InitialBalanceWarningTimeout = time.Minute * 10

	// SetAllowanceWarningTimeout defines how long the renter will wait before
	// reporting to the user that the allowance has not yet been set
	// successfully.
	SetAllowanceWarningTimeout = time.Minute * 2

	// SetAllowanceFrequency defines how frequently the renter job tries to set
	// renter allowance
	SetAllowanceFrequency = time.Second * 15

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
	maxUploadTime = time.Minute * 10

	// uploadFileCheckFrequency defines how frequently the renter job checks if
	// file upload has reached 100%
	uploadFileCheckFrequency = time.Second * 20

	// renterAllowancePeriod defines the block duration of the renter's allowance
	renterAllowancePeriod = 100

	// renterDataPieces defines the number of data pieces per erasure-coded chunk
	renterDataPieces = 1

	// renterParityPieces defines the number of parity pieces per erasure-coded
	renterParityPieces = 4

	// renterSourceFilesDir defines source directory for renter uploads
	renterSourceFilesDir = "renterSourceFiles"

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
	downloadFileTimeout = time.Minute * 15

	// downloadFileFrequency defines how frequently the renter job checks if a
	// file is downloaded
	downloadFileCheckFrequency = time.Second

	// BalanceCheckFrequency defines how frequently the renter job checks if
	// minimum treshold of coins have been mined
	BalanceCheckFrequency = time.Second * 5
)

var (
	// requiredInitialBalance sets the number of coins that the renter requires
	// before uploading will begin.
	requiredInitialBalance = types.NewCurrency64(100e3).Mul(types.SiacoinPrecision)

	// DefaultAntfarmAllowance is the set of allowance settings that will be
	// used by renter
	DefaultAntfarmAllowance = modules.Allowance{
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

	StaticJR *JobRunner
	mu       sync.Mutex
}

// MerkleRoot calculates merkle root of the file given in reader
func MerkleRoot(r io.Reader) (h crypto.Hash, err error) {
	root, err := merkletree.ReaderRoot(r, crypto.NewHash(), crypto.SegmentSize)
	copy(h[:], root)
	return
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

// randFillFile will append 'size' bytes to the input file, returning the
// merkle root of the bytes that were appended.
func randFillFile(f *os.File, size uint64) (h crypto.Hash, err error) {
	tee := io.TeeReader(io.LimitReader(fastrand.Reader, int64(size)), f)
	h, err = MerkleRoot(tee)
	return
}

// storageRenter unlocks the wallet, mines some currency, sets an allowance
// using that currency, and uploads some files.  It will periodically try to
// download or delete those files, printing any errors that occur.
func (j *JobRunner) storageRenter() {
	j.StaticTG.Add()
	defer j.StaticTG.Done()

	// Wait for ants to be synced
	AntSyncWG.Wait()

	// Wait for balance to be filled
	j.BlockUntilWalletIsFilled("renter", requiredInitialBalance, BalanceCheckFrequency, InitialBalanceWarningTimeout)

	// Set allowance.
	rj := RenterJob{
		StaticJR: j,
	}
	err := rj.SetAllowance(DefaultAntfarmAllowance, SetAllowanceFrequency, SetAllowanceWarningTimeout)
	if err != nil {
		log.Printf("[ERROR] [renter] [%v] Trouble when setting renter allowance: %v\n", j.StaticSiaDirectory, err)
	}

	// Spawn the uploader and downloader threads.
	go rj.threadedUploader()
	go rj.threadedDownloader()
	go rj.threadedDeleter()
}

// CreateSourceFilesDir creates renter's source files directory from which
// files can be uploaded
func (r *RenterJob) CreateSourceFilesDir() error {
	err := os.Mkdir(filepath.Join(r.managedSiaDirectory(), renterSourceFilesDir), 0700)
	if err != nil {
		return errors.AddContext(err, "couldn't create renter source files directory")
	}
	return nil
}

// DisableIPViolationCheck disables IP violation check for renter so that
// renter can rent on multiple hosts within on the same IP subnet
func (r *RenterJob) DisableIPViolationCheck() error {
	siaDir := r.managedSiaDirectory()
	log.Printf("[INFO] [renter] [%v] Disabling IP violation check...\n", siaDir)
	// Set checkforipviolation=false
	values := url.Values{}
	values.Set("checkforipviolation", "false")
	err := r.managedClient().RenterPost(values)
	if err != nil {
		return errors.AddContext(err, "couldn't set checkforipviolation")
	}
	log.Printf("[INFO] [renter] [%v] Disabled IP violation check.\n", siaDir)
	return nil
}

// managedClient gets renter's jobRunners's http client
func (r *RenterJob) managedClient() *client.Client {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.StaticJR.staticClient
}

// managedDeleteRandom deletes a random file from the renter
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
	if err := r.StaticJR.staticClient.RenterFileDeletePost(path); err != nil {
		return err
	}

	log.Printf("[%v jobStorageRenter INFO]: successfully deleted file\n", r.StaticJR.StaticSiaDirectory)
	os.Remove(r.Files[randindex].SourceFile)
	r.Files = append(r.Files[:randindex], r.Files[randindex+1:]...)

	return nil
}

// ManagedDownload downloads the given file
func (r *RenterJob) ManagedDownload(fileToDownload modules.FileInfo) (*os.File, error) {
	r.managedJobRunnerThreadGroupAdd()
	defer r.managedJobRunnerThreadGroupDone()

	// Use ioutil.TempFile to get a random temporary filename.
	f, err := ioutil.TempFile("", "antfarm-renter")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary file for download: %v", err)
	}
	defer f.Close()
	destPath, _ := filepath.Abs(f.Name())
	os.Remove(destPath)

	siaDir := r.managedSiaDirectory()
	log.Printf("[INFO] [renter] [%v] downloading %v to %v", siaDir, fileToDownload.SiaPath, destPath)

	_, err = r.managedClient().RenterDownloadGet(fileToDownload.SiaPath, destPath, 0, fileToDownload.Filesize, true, false)
	if err != nil {
		return nil, fmt.Errorf("failed in call to /renter/download: %v", err)
	}

	// Wait for the file to appear in the download list
	success := false
	for start := time.Now(); time.Since(start) < fileAppearInDownloadListTimeout; {
		select {
		case <-r.managedJobRunnerThreadGroupStopChan():
			return nil, nil
		case <-time.After(fileApearInDownloadListFrequency):
		}

		hasFile, _, err := isFileInDownloads(r.managedClient(), fileToDownload)
		if err != nil {
			return nil, fmt.Errorf("error waiting for the file to appear in the download queue: %v", err)
		}
		if hasFile {
			success = true
			break
		}
	}
	if !success {
		return nil, fmt.Errorf("file %v did not appear in the renter download queue", fileToDownload.SiaPath)
	}

	// Wait for the file to be finished downloading, with a timeout of 15 minutes.
	success = false
	for start := time.Now(); time.Since(start) < downloadFileTimeout; {
		select {
		case <-r.managedJobRunnerThreadGroupStopChan():
			return nil, nil
		case <-time.After(downloadFileCheckFrequency):
		}

		hasFile, info, err := isFileInDownloads(r.managedClient(), fileToDownload)
		if err != nil {
			return nil, fmt.Errorf("error waiting for the file to disappear from the download queue: %v", err)
		}
		if hasFile && info.Received == info.Filesize {
			success = true
			break
		} else if !hasFile {
			log.Printf("[INFO] [renter] [%v]: file unexpectedly missing from download list\n", siaDir)
		} else {
			log.Printf("[INFO] [renter] [%v]: currently downloading %v, received %v bytes\n", siaDir, fileToDownload.SiaPath, info.Received)
		}
	}
	if !success {
		return nil, fmt.Errorf("file %v did not complete downloading", fileToDownload.SiaPath)
	}
	log.Printf("[INFO] [renter] [%v]: successfully downloaded %v to %v\n", siaDir, fileToDownload.SiaPath, destPath)
	return f, nil
}

// managedDownloadRandom will managedDownload a random file from the network.
func (r *RenterJob) managedDownloadRandom() error {
	r.managedJobRunnerThreadGroupAdd()
	defer r.managedJobRunnerThreadGroupDone()

	// Download a random file from the renter's file list
	renterFiles, err := r.managedClient().RenterFilesGet(false) // cached=false
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
	_, err = r.ManagedDownload(fileToDownload)
	return err
}

// managedJobRunnerThreadGroupAdd increments renter's jobRunner's thread group
// counter
func (r *RenterJob) managedJobRunnerThreadGroupAdd() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.StaticJR.StaticTG.Add()
}

// managedJobRunnerThreadGroupDone decrements renter's jobRunner's thread group
// counter
func (r *RenterJob) managedJobRunnerThreadGroupDone() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.StaticJR.StaticTG.Done()
}

// managedJobRunnerThreadGroupStopChan managed calls renter's jobRunner's
// thread group to interrupt long running reads
func (r *RenterJob) managedJobRunnerThreadGroupStopChan() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.StaticJR.StaticTG.StopChan()
}

// managedSiaDirectory returns renter's jobRunner's Sia directory
func (r *RenterJob) managedSiaDirectory() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.StaticJR.StaticSiaDirectory
}

// ManagedUpload will ManagedUpload a file to the network. If the api reports that there are
// more than 10 files successfully uploaded, then a file is deleted at random.
func (r *RenterJob) ManagedUpload(uploadFileSize uint64) error {
	r.managedJobRunnerThreadGroupAdd()
	defer r.managedJobRunnerThreadGroupDone()

	// Generate some random data to upload. The file needs to be closed before
	// the upload to the network starts, so this code is wrapped in a func such
	// that a `defer Close()` can be used on the file.
	siaDir := r.managedSiaDirectory()
	log.Printf("[INFO] [renter] [%v] File upload preparation beginning.\n", siaDir)
	var sourcePath string
	var merkleRoot crypto.Hash
	err := func() error {
		f, err := ioutil.TempFile(filepath.Join(siaDir, "renterSourceFiles"), "renterFile")
		if err != nil {
			return fmt.Errorf("unable to open tmp file for renter source file: %v", err)
		}
		defer f.Close()
		sourcePath, _ = filepath.Abs(f.Name())

		// Fill the file with random data.
		merkleRoot, err = randFillFile(f, uploadFileSize)
		if err != nil {
			return fmt.Errorf("unable to fill file with randomness: %v", err)
		}
		return nil
	}()
	if err != nil {
		return err
	}

	siapath, err := modules.NewSiaPath(sourcePath)
	if err != nil {
		return err
	}

	// Add the file to the renter.
	rf := RenterFile{
		MerkleRoot: merkleRoot,
		SourceFile: sourcePath,
	}
	r.mu.Lock()
	r.Files = append(r.Files, rf)
	r.mu.Unlock()
	log.Printf("[INFO] [renter] [%v] File upload preparation complete, beginning file upload.\n", siaDir)

	// Upload the file to the network.
	if err := r.StaticJR.staticClient.RenterUploadPost(sourcePath, siapath, renterDataPieces, renterParityPieces); err != nil {
		return fmt.Errorf("unable to upload file to network: %v", err)
	}
	log.Printf("[INFO] [renter] [%v] /renter/upload call completed successfully.  Waiting for the upload to complete\n", siaDir)

	// Block until the upload has reached 100%.
	uploadProgress := 0.0
	for start := time.Now(); time.Since(start) < maxUploadTime; {
		select {
		case <-r.managedJobRunnerThreadGroupStopChan():
			return nil
		case <-time.After(uploadFileCheckFrequency):
		}

		rfg, err := r.managedClient().RenterFilesGet(false) // cached=false
		if err != nil {
			return fmt.Errorf("error calling /renter/files: %v", err)
		}

		for _, file := range rfg.Files {
			if file.SiaPath == siapath {
				uploadProgress = file.UploadProgress
			}
		}
		log.Printf("[INFO] [renter] [%v]: upload progress: %v%%\n", siaDir, uploadProgress)
		if uploadProgress == 100 {
			break
		}
	}
	if uploadProgress < 100 {
		return fmt.Errorf("file with siapath %v could not be fully uploaded after 10 minutes.  progress reached: %v", siapath, uploadProgress)
	}
	log.Printf("[INFO] [renter] [%v]: file has been successfully uploaded to 100%%.\n", siaDir)
	return nil
}

// threadedDeleter deletes one random file from the renter every 100 seconds
// once 10 or more files have been uploaded.
func (r *RenterJob) threadedDeleter() {
	for {
		select {
		case <-r.managedJobRunnerThreadGroupStopChan():
			return
		case <-time.After(deleteFileFrequency):
		}

		if err := r.managedDeleteRandom(); err != nil {
			log.Printf("[ERROR] [renter] [%v]: %v\n", r.managedSiaDirectory(), err)
		}
	}
}

// threadedDownloader is a function that continuously runs for the renter job,
// downloading a file at random every 400 seconds.
func (r *RenterJob) threadedDownloader() {
	// Wait for the first file to be uploaded before starting the download
	// loop.
	for {
		select {
		case <-r.managedJobRunnerThreadGroupStopChan():
			return
		case <-time.After(downloadFileFrequency):
		}

		// Download a file.
		if err := r.managedDownloadRandom(); err != nil {
			log.Printf("[ERROR] [renter] [%v]: %v\n", r.managedSiaDirectory(), err)
		}
	}
}

// threadedUploader is a function that continuously runs for the renter job,
// uploading a 500MB file every 240 seconds (10 blocks). The renter should have
// already set an allowance.
func (r *RenterJob) threadedUploader() {
	// Make the source files directory
	err := r.CreateSourceFilesDir()
	if err != nil {
		panic(err)
	}
	for {
		// Wait a while between upload attempts.
		select {
		case <-r.managedJobRunnerThreadGroupStopChan():
			return
		case <-time.After(uploadFileFrequency):
		}

		// Upload a file.
		if err := r.ManagedUpload(uploadFileSize); err != nil {
			log.Printf("[ERROR] [renter] [%v]: %v\n", r.managedSiaDirectory(), err)
		}
	}
}

// SetAllowance sets renter's allowance in a loop with timeout and frequency
func (r *RenterJob) SetAllowance(allowance modules.Allowance, frequency, timeout time.Duration) error {
	// Block until a renter allowance has successfully been set.
	start := time.Now()
	siaDir := r.managedSiaDirectory()
	for {
		log.Printf("[DEBUG] [renter] [%v] Attempting to set allowance.\n", siaDir)
		err := r.managedClient().RenterPostAllowance(allowance)
		log.Printf("[DEBUG] [renter] [%v] Allowance attempt complete: %v\n", siaDir, err)
		if err == nil {
			// Success, we can exit the loop.
			break
		}
		if err != nil && time.Since(start) > timeout {
			return err
		}

		// Wait a bit before trying again.
		select {
		case <-r.managedJobRunnerThreadGroupStopChan():
			return nil
		case <-time.After(frequency):
		}
	}
	log.Printf("[INFO] [renter] [%v] Renter allowance has been set successfully.\n", siaDir)
	return nil
}

// WaitForUploadReady waits given timeout until renter is upload ready
func (r *RenterJob) WaitForUploadReady() error {
	siaDir := r.managedSiaDirectory()
	log.Printf("[INFO] [renter] [%v] Waiting for renter is upload ready.\n", siaDir)
	timeout := time.Minute * 5
	frequency := time.Second * 5
	tries := int(timeout / frequency)
	err := build.Retry(tries, frequency, func() error {
		rur, err := r.managedClient().RenterUploadReadyGet(renterDataPieces, renterParityPieces)
		if err != nil {
			return err
		}
		if !rur.Ready {
			return errors.New("renter is not upload ready")
		}
		return nil
	})
	if err != nil {
		msg := fmt.Sprintf("[ERROR] [renter] [%v]: renter is not upload ready within %v timeout", siaDir, timeout)
		return errors.New(msg)
	}
	log.Printf("[INFO] [renter] [%v] Renter is upload ready.\n", siaDir)
	return nil
}
