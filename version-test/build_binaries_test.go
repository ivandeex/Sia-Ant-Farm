package versiontest

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const (
	// minReleaseVersion defines minimum Sia release version to include in
	// build binaries tests.
	minReleaseVersion = "v1.3.7"
)

// TestBuildBinaries builds siad-dev binaries according to the settings in
// const.
func TestBuildBinaries(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	releases, err := getReleases(minReleaseVersion)
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

	// Check binary files
	for _, release := range releases {
		releasePath := filepath.Join(binariesDir, fmt.Sprintf("Sia-%v-linux-amd64", release), "siad-dev")
		_, err := os.Stat(releasePath)
		if err != nil && os.IsNotExist(err) {
			t.Errorf("Expected binary %v was not found\n", releasePath)
		} else if err != nil {
			t.Fatal(err)
		}
	}
}

// TestGetReleases checks if we get correct Sia releases from Sia Gitlab
// repository. Some official Sia releases need patches to work with Sia Antfarm
// so we need to build and test on patched version having '-antfarm' git tag
// suffix.
func TestGetReleases(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	releases, err := getReleases(minReleaseVersion)
	if err != nil {
		t.Fatal(err)
	}

	// Check returned releases from Gitlab API. The beginning of the releases
	// slice is checked, newer releases can be added to Sia repo and the test
	// still passes.
	expectedReleases := []string{
		"v1.3.7", "v1.4.0", "v1.4.1", "v1.4.1.1", "v1.4.1.2", "v1.4.2.0",
		"v1.4.3", "v1.4.4-antfarm", "v1.4.5-antfarm", "v1.4.6-antfarm",
		"v1.4.7-antfarm", "v1.4.8-antfarm", "v1.4.10-antfarm",
		"v1.4.11-antfarm", "v1.5.0",
	}
	for i := range expectedReleases {
		if releases[i] != expectedReleases[i] {
			t.Fatalf("Expected to get release %v, got release %v\n", expectedReleases[i], releases[i])
		}
	}
}
