package fileutils

import (
	"fmt"
	"os"
	"time"

	"go.sia.tech/siad/build"
	"gitlab.com/NebulousLabs/errors"
)

// WaitForFileComplete blocks until the file has expected size.
func WaitForFileComplete(filetPath string, fileSize int64, timeout time.Duration) error {
	// frequency defines time interval how often we check for the file to
	// become complete/fully synced to disk.
	frequency := time.Millisecond * 100
	tries := int(timeout/frequency) + 1
	return build.Retry(tries, frequency, func() error {
		fi, err := os.Stat(filetPath)
		if err != nil {
			return errors.AddContext(err, "can't open destination path")
		}
		if fi.Size() != fileSize {
			return fmt.Errorf("file size %v doesn't match expected file size %v", fi.Size(), fileSize)
		}
		return nil
	})
}
