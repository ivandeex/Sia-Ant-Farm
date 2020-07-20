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

	// setAllowanceWarningTimeout defines how long the renter will wait before
	// reporting to the user that the allowance has not yet been set
	// successfully.
	setAllowanceWarningTimeout = time.Minute * 2

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

	// balanceCheckFrequency defines how frequently the renter job checks if
	// minimum treshold of coins have been mined
	balanceCheckFrequency = time.Second * 15
)

var (
	// requiredInitialBalance sets the number of coins that the renter requires
	// before uploading will begin.
	requiredInitialBalance = types.NewCurrency64(100e3).Mul(types.SiacoinPrecision)

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
)

// renterFile stores the location and checksum of a file active on the renter.
type renterFile struct {
	merkleRoot crypto.Hash
	sourceFile string
}

// renterJob contains statefulness that is used to drive the renter. Most
// importantly, it contains a list of files that the renter is currently
// uploading to the network.
type renterJob struct {
	files []renterFile

	staticJR *JobRunner
	mu       sync.Mutex
}

// randFillFile will append 'size' bytes to the input file, returning the
// merkle root of the bytes that were appended.
func randFillFile(f *os.File, size uint64) (h crypto.Hash, err error) {
	tee := io.TeeReader(io.LimitReader(fastrand.Reader, int64(size)), f)
	root, err := merkletree.ReaderRoot(tee, crypto.NewHash(), crypto.SegmentSize)
	copy(h[:], root)
	return
}

// threadedDownloader is a function that continuously runs for the renter job,
// downloading a file at random every 400 seconds.
func (r *renterJob) threadedDownloader() {
	// Wait for the first file to be uploaded before starting the download
	// loop.
	for {
		select {
		case <-r.staticJR.StaticTG.StopChan():
			return
		case <-time.After(downloadFileFrequency):
		}

		// Download a file.
		if err := r.managedDownload(); err != nil {
			log.Printf("[ERROR] [renter] [%v]: %v\n", r.staticJR.staticSiaDirectory, err)
		}
	}
}

// threadedUploader is a function that continuously runs for the renter job,
// uploading a 500MB file every 240 seconds (10 blocks). The renter should have
// already set an allowance.
func (r *renterJob) threadedUploader() {
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
		if err := r.managedUpload(); err != nil {
			log.Printf("[ERROR] [renter] [%v]: %v\n", r.staticJR.staticSiaDirectory, err)
		}
	}
}

// threadedDeleter deletes one random file from the renter every 100 seconds
// once 10 or more files have been uploaded.
func (r *renterJob) threadedDeleter() {
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

// managedDeleteRandom deletes a random file from the renter.
func (r *renterJob) managedDeleteRandom() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// no-op with fewer than 10 files
	if len(r.files) < deleteFileThreshold {
		return nil
	}

	randindex := fastrand.Intn(len(r.files))

	path, err := modules.NewSiaPath(r.files[randindex].sourceFile)
	if err != nil {
		return err
	}
	if err := r.staticJR.staticClient.RenterFileDeletePost(path); err != nil {
		return err
	}

	log.Printf("[%v jobStorageRenter INFO]: successfully deleted file\n", r.staticJR.staticSiaDirectory)
	os.Remove(r.files[randindex].sourceFile)
	r.files = append(r.files[:randindex], r.files[randindex+1:]...)

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

// managedDownload will managedDownload a random file from the network.
func (r *renterJob) managedDownload() error {
	r.staticJR.StaticTG.Add()
	defer r.staticJR.StaticTG.Done()

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
	defer f.Close()
	destPath, _ := filepath.Abs(f.Name())
	os.Remove(destPath)

	log.Printf("[INFO] [renter] [%v] downloading %v to %v", r.staticJR.staticSiaDirectory, fileToDownload.SiaPath, destPath)

	_, err = r.staticJR.staticClient.RenterDownloadGet(fileToDownload.SiaPath, destPath, 0, fileToDownload.Filesize, true, false)
	if err != nil {
		return fmt.Errorf("failed in call to /renter/download: %v", err)
	}

	// Wait for the file to appear in the download list
	success := false
	for start := time.Now(); time.Since(start) < fileAppearInDownloadListTimeout; {
		select {
		case <-r.staticJR.StaticTG.StopChan():
			return nil
		case <-time.After(fileApearInDownloadListFrequency):
		}

		hasFile, _, err := isFileInDownloads(r.staticJR.staticClient, fileToDownload)
		if err != nil {
			return fmt.Errorf("error waiting for the file to appear in the download queue: %v", err)
		}
		if hasFile {
			success = true
			break
		}
	}
	if !success {
		return fmt.Errorf("file %v did not appear in the renter download queue", fileToDownload.SiaPath)
	}

	// Wait for the file to be finished downloading, with a timeout of 15 minutes.
	success = false
	for start := time.Now(); time.Since(start) < downloadFileTimeout; {
		select {
		case <-r.staticJR.StaticTG.StopChan():
			return nil
		case <-time.After(downloadFileCheckFrequency):
		}

		hasFile, info, err := isFileInDownloads(r.staticJR.staticClient, fileToDownload)
		if err != nil {
			return fmt.Errorf("error waiting for the file to disappear from the download queue: %v", err)
		}
		if hasFile && info.Received == info.Filesize {
			success = true
			break
		} else if !hasFile {
			log.Printf("[INFO] [renter] [%v]: file unexpectedly missing from download list\n", r.staticJR.staticSiaDirectory)
		} else {
			log.Printf("[INFO] [renter] [%v]: currently downloading %v, received %v bytes\n", r.staticJR.staticSiaDirectory, fileToDownload.SiaPath, info.Received)
		}
	}
	if !success {
		return fmt.Errorf("file %v did not complete downloading", fileToDownload.SiaPath)
	}
	log.Printf("[INFO] [renter] [%v]: successfully downloaded %v to %v\n", r.staticJR.staticSiaDirectory, fileToDownload.SiaPath, destPath)
	return nil
}

// managedUpload will managedUpload a file to the network. If the api reports that there are
// more than 10 files successfully uploaded, then a file is deleted at random.
func (r *renterJob) managedUpload() error {
	r.staticJR.StaticTG.Add()
	defer r.staticJR.StaticTG.Done()

	// Generate some random data to upload. The file needs to be closed before
	// the upload to the network starts, so this code is wrapped in a func such
	// that a `defer Close()` can be used on the file.
	log.Printf("[INFO] [renter] [%v] File upload preparation beginning.\n", r.staticJR.staticSiaDirectory)
	var sourcePath string
	var merkleRoot crypto.Hash
	success, err := func() (bool, error) {
		f, err := ioutil.TempFile(filepath.Join(r.staticJR.staticSiaDirectory, "renterSourceFiles"), "renterFile")
		if err != nil {
			return false, fmt.Errorf("unable to open tmp file for renter source file: %v", err)
		}
		defer f.Close()
		sourcePath, _ = filepath.Abs(f.Name())

		// Fill the file with random data.
		merkleRoot, err = randFillFile(f, uploadFileSize)
		if err != nil {
			return false, fmt.Errorf("unable to fill file with randomness: %v", err)
		}
		return true, nil
	}()
	if !success {
		return err
	}

	siapath, err := modules.NewSiaPath(sourcePath)
	if err != nil {
		return err
	}

	// Add the file to the renter.
	rf := renterFile{
		merkleRoot: merkleRoot,
		sourceFile: sourcePath,
	}
	r.mu.Lock()
	r.files = append(r.files, rf)
	r.mu.Unlock()
	log.Printf("[INFO] [renter] [%v] File upload preparation complete, beginning file upload.\n", r.staticJR.staticSiaDirectory)

	// Upload the file to the network.
	if err := r.staticJR.staticClient.RenterUploadPost(sourcePath, siapath, renterDataPieces, renterParityPieces); err != nil {
		return fmt.Errorf("unable to upload file to network: %v", err)
	}
	log.Printf("[INFO] [renter] [%v] /renter/upload call completed successfully.  Waiting for the upload to complete\n", r.staticJR.staticSiaDirectory)

	// Block until the upload has reached 100%.
	uploadProgress := 0.0
	for start := time.Now(); time.Since(start) < maxUploadTime; {
		select {
		case <-r.staticJR.StaticTG.StopChan():
			return nil
		case <-time.After(uploadFileCheckFrequency):
		}

		rfg, err := r.staticJR.staticClient.RenterFilesGet(false) // cached=false
		if err != nil {
			return fmt.Errorf("error calling /renter/files: %v", err)
		}

		for _, file := range rfg.Files {
			if file.SiaPath == siapath {
				uploadProgress = file.UploadProgress
			}
		}
		log.Printf("[INFO] [renter] [%v]: upload progress: %v%%\n", r.staticJR.staticSiaDirectory, uploadProgress)
		if uploadProgress == 100 {
			break
		}
	}
	if uploadProgress < 100 {
		return fmt.Errorf("file with siapath %v could not be fully uploaded after 10 minutes.  progress reached: %v", siapath, uploadProgress)
	}
	log.Printf("[INFO] [renter] [%v]: file has been successfully uploaded to 100%%.\n", r.staticJR.staticSiaDirectory)
	return nil
}

// storageRenter unlocks the wallet, mines some currency, sets an allowance
// using that currency, and uploads some files.  It will periodically try to
// download or delete those files, printing any errors that occur.
func (j *JobRunner) storageRenter() {
	j.StaticTG.Add()
	defer j.StaticTG.Done()

	// Wait for ants to be synced
	AntSyncWG.Wait()

	// Block until a minimum threshold of coins have been mined.
	start := time.Now()
	log.Printf("[INFO] [renter] [%v] Blocking until wallet is sufficiently full\n", j.staticSiaDirectory)
	for {
		// Get the wallet balance.
		walletInfo, err := j.staticClient.WalletGet()
		if err != nil {
			log.Printf("[ERROR] [renter] [%v] Trouble when calling /wallet: %v\n", j.staticSiaDirectory, err)

			// Wait before trying to get the balance again.
			select {
			case <-j.StaticTG.StopChan():
				return
			case <-time.After(balanceCheckFrequency):
			}
			continue
		}

		// Break the wait loop when we have enough balance.
		if walletInfo.ConfirmedSiacoinBalance.Cmp(requiredInitialBalance) > 0 {
			break
		}

		// Log an error if the time elapsed has exceeded the warning threshold.
		if time.Since(start) > initialBalanceWarningTimeout {
			log.Printf("[ERROR] [renter] [%v] Minimum balance for allowance has not been reached. Time elapsed: %v\n", j.staticSiaDirectory, time.Since(start))
		}

		// Wait before trying to get the balance again.
		select {
		case <-j.StaticTG.StopChan():
			return
		case <-time.After(balanceCheckFrequency):
		}
	}
	log.Printf("[INFO] [renter] [%v] Wallet filled successfully. Blocking until allowance has been set.\n", j.staticSiaDirectory)

	// Block until a renter allowance has successfully been set.
	start = time.Now()
	for {
		log.Printf("[DEBUG] [renter] [%v] Attempting to set allowance.\n", j.staticSiaDirectory)
		err := j.staticClient.RenterPostAllowance(allowance)
		log.Printf("[DEBUG] [renter] [%v] Allowance attempt complete: %v\n", j.staticSiaDirectory, err)
		if err == nil {
			// Success, we can exit the loop.
			break
		}
		if err != nil && time.Since(start) > setAllowanceWarningTimeout {
			log.Printf("[ERROR] [renter] [%v] Trouble when setting renter allowance: %v\n", j.staticSiaDirectory, err)
		}

		// Wait a bit before trying again.
		select {
		case <-j.StaticTG.StopChan():
			return
		case <-time.After(setAllowanceFrequency):
		}
	}
	log.Printf("[INFO] [renter] [%v] Renter allowance has been set successfully.\n", j.staticSiaDirectory)

	// Spawn the uploader and downloader threads.
	rj := renterJob{
		staticJR: j,
	}

	go rj.threadedUploader()
	go rj.threadedDownloader()
	go rj.threadedDeleter()
}
