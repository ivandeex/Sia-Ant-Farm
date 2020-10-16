package utils

import (
	"bufio"
	"os"
	"strings"

	"gitlab.com/NebulousLabs/errors"
)

// FileContains returns true if the given file contains a line with the given
// string.
func FileContains(filepath, s string) (bool, error) {
	f, err := os.Open(filepath)
	if err != nil {
		return false, err
	}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), s) {
			return true, nil
		}
	}
	if err = scanner.Err(); err != nil {
		return false, errors.AddContext(err, "error scanning a file")
	}
	return false, nil
}
