package versiontest

import (
	"testing"
)

const (
	// binariesDir defines path where build binaries should be stored. If the
	// path is set as relative, it is relative to Sia-Ant-Farm/version-test
	// directory.
	binariesDir = "../upgrade-binaries"

	// minVersion defines minimum released Sia version to include in built and
	// tested binaries.
	minVersion = "v1.3.7"

	// rebuildReleaseBinaries defines whether the release siad binaries should
	// be rebuilt. It can be set to false when rerunning the test(s) on already
	// built binaries.
	rebuildReleaseBinaries = true

	// rebuildMaster defines whether the newest Sia master siad binary should
	// be rebuilt. It can be set to false when rerunning the test(s) on already
	// build binary.
	rebuildMaster = true
)

// TestBuildBinaries builds siad-dev binaries according to the settings in
// const. It requires Sia repository to be present in the standard location in
// GOPATH. The test is not a part of standard Sia Ant Farm Gitlab CI tests.
func TestBuildBinaries(t *testing.T) {
	releases, err := getReleases(minVersion)
	if err != nil {
		t.Fatal(err)
	}

	// Build release binaries
	if rebuildReleaseBinaries {
		err = buildSiad(binariesDir, releases...)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Build master binary
	if rebuildMaster {
		err := buildSiad(binariesDir, "master")
		if err != nil {
			t.Fatal(err)
		}
	}
}
