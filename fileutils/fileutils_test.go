package fileutils

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.sia.tech/sia-antfarm/persist"
	"go.sia.tech/sia-antfarm/test"
	"gitlab.com/NebulousLabs/fastrand"
)

const (
	// fileSize defines a file size to test on
	fileSize = 1e6 // 1 MB

	// frequency defines how often to append to test file
	frequency = time.Millisecond * 50

	// speed defines speed of appending to the test file in bytes per second
	speed = int(1e5)
)

// TestWaitForFileCompleteReady tests that WaitForFileComplete check for ready
// file is almost instant.
func TestWaitForFileCompleteReady(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Get test dir
	dataDir := test.TestDir(t.Name())

	// Prepare file
	fp := filepath.Join(dataDir, "ready-file")
	data := fastrand.Bytes(fileSize)
	err := ioutil.WriteFile(fp, data[:], 0600)
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	err = WaitForFileComplete(fp, fileSize, time.Millisecond*50)
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if elapsed > time.Millisecond*50 {
		t.Fatalf("it took too long (%v) to wait for ready file", elapsed)
	}
}

// TestWaitForFileCompleteSyncing tests that WaitForFileComplete check waits
// correct time for file being saved to disk.
func TestWaitForFileCompleteSyncing(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Get test dir
	dataDir := test.TestDir(t.Name())

	// Create logger
	logger := test.NewTestLogger(t, dataDir)
	defer func() {
		if err := logger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Prepare file
	fp := filepath.Join(dataDir, "syncing-file")
	data := fastrand.Bytes(fileSize)
	f, err := os.OpenFile(fp, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			logger.Errorf("can't close test file: %v", err)
		}
	}()

	// Write the file slowly
	var start time.Time
	go func() {
		start = time.Now()
		appendToFile(logger, f, speed, frequency, data[:], make(chan struct{}))
	}()

	// Wait
	err = WaitForFileComplete(fp, fileSize, time.Second*11)
	if err != nil {
		t.Fatalf("error waiting for the file to become complete: %v", err)
	}

	elapsed := time.Since(start)
	if elapsed < time.Second*10 {
		t.Fatalf("waiting for file to become complete was too short: %v", elapsed)
	}
	if elapsed > time.Second*11 {
		t.Fatalf("waiting for the file to become complete took too long: %v", elapsed)
	}
}

// TestWaitForFileCompleteTimeout tests that WaitForFileComplete timeouts
// correctly.
func TestWaitForFileCompleteTimeout(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Get test dir
	dataDir := test.TestDir(t.Name())

	// Create logger
	logger := test.NewTestLogger(t, dataDir)
	defer func() {
		if err := logger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Prepare file
	fp := filepath.Join(dataDir, "timeout-file")
	data := fastrand.Bytes(fileSize)
	f, err := os.OpenFile(fp, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	defer func() {
		close(stop)
		if err := f.Close(); err != nil {
			logger.Errorf("can't close test file: %v", err)
		}
	}()

	// Write the file slowly
	go appendToFile(logger, f, speed, frequency, data[:], stop)

	// Wait
	err = WaitForFileComplete(fp, fileSize, time.Second*9)

	// Check there was a correct error
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "doesn't match expected file size") {
		t.Fatalf("expected timeout error, got: %v", err)
	}
}

// appendToFile is a helper function that gradually appends data to the file at
// the given speed and frequency.
func appendToFile(logger *persist.Logger, f *os.File, speed int, frequency time.Duration, data []byte, stop <-chan struct{}) {
	// Calculate how many bytes to write per each iteration
	b := speed * int(frequency) / int(time.Second)
	l := len(data)
	for written := 0; written < l; written += b {
		// Wait (limit appending speed)
		select {
		case <-stop:
			return
		case <-time.After(frequency):
		}

		// Write to the file in a goroutine not to slowdown the timing
		go func(written int) {
			// Fix higher data slice bound
			top := written + b
			if top > l {
				top = l
			}

			// Write some data
			_, err := f.Write(data[written:top])
			if err != nil {
				logger.Errorf("cant write data to test file: %v", err)
			}
			err = f.Sync()
			if err != nil {
				logger.Errorf("can't sync test file: %v", err)
			}
		}(written)
	}
}
