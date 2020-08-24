package versiontest

import (
	"testing"
)

// TestBuildBinaries builds siad-dev binaries according to the settings in
// const.
func TestBuildBinaries(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	minVersion := "v1.3.7"
	releases, err := getReleases(minVersion)
	if err != nil {
		t.Fatal(err)
	}

	releases = append(releases, "master")

	// Build release binaries
	binariesDir := "../upgrade-binaries"
	err = buildSiad(binariesDir, releases...)
	if err != nil {
		t.Fatal(err)
	}
}
