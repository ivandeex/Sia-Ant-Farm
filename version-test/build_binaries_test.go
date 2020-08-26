package versiontest

import (
	"fmt"
	"os"
	"path/filepath"
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

	// Check returned releases from Gitlab API (the beginning of the releases
	// slice, newer releases can be added and the test will still pass)
	expectedReleases := []string{"v1.3.7", "v1.4.0", "v1.4.1", "v1.4.1.1", "v1.4.1.2", "v1.4.2.0", "v1.4.3", "v1.4.4", "v1.4.5", "v1.4.6-antfarm", "v1.4.7-antfarm", "v1.4.8-antfarm", "v1.4.10", "v1.4.11", "v1.5.0"}
	for i := range expectedReleases {
		if releases[i] != expectedReleases[i] {
			t.Fatalf("Expected to get release %v, got release %v\n", expectedReleases[i], releases[i])
		}
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
