package binariesbuilder

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"go.sia.tech/sia-antfarm/test"
)

const (
	// minReleaseVersion defines minimum Sia release version to include in
	// build binaries tests.
	minReleaseVersion = "v1.3.7"
)

type excludeVersionTestConfig struct {
	testName         string
	givenVersions    []string
	excludeVersions  []string
	expectedVersions []string
}

// TestBuildBinaries builds siad-dev binaries according to the settings in
// const.
func TestBuildBinaries(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	releases, err := GetReleases(minReleaseVersion)
	if err != nil {
		t.Fatal(err)
	}
	releases = append(releases, "master")

	// Prepare logger
	dataDir := test.TestDir(t.Name())
	logger := test.NewTestLogger(t, dataDir)
	defer func() {
		if err := logger.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Build release binaries
	err = buildSiad(logger, BinariesDir, releases...)
	if err != nil {
		t.Fatal(err)
	}

	// Check binary files
	for _, release := range releases {
		releasePath := filepath.Join(BinariesDir, fmt.Sprintf("Sia-%v-linux-amd64", release), "siad-dev")
		_, err := os.Stat(releasePath)
		if err != nil && os.IsNotExist(err) {
			t.Errorf("Expected binary %v was not found\n", releasePath)
		} else if err != nil {
			t.Fatal(err)
		}
	}
}

// TestExcludeVersions checks correctness of excludeVersions function.
func TestExcludeVersions(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	tests := []excludeVersionTestConfig{
		{
			testName:         "TestEmptyVersions",
			givenVersions:    []string{},
			excludeVersions:  []string{"v1.2.3", "master"},
			expectedVersions: []string{},
		},
		{
			testName:         "TestEmptyExcludeVersions",
			givenVersions:    []string{"v1.2.3", "master"},
			excludeVersions:  []string{},
			expectedVersions: []string{"v1.2.3", "master"},
		},
		{
			testName:         "TestNoIntersectionVersions",
			givenVersions:    []string{"v1.2.3", "master"},
			excludeVersions:  []string{"v1.0.0", "v1.1.1"},
			expectedVersions: []string{"v1.2.3", "master"},
		},
		{
			testName:         "TestExcludeSomeVersions",
			givenVersions:    []string{"v1.0.0", "v1.1.1", "v1.2.3", "master"},
			excludeVersions:  []string{"v1.0.0", "v1.2.3"},
			expectedVersions: []string{"v1.1.1", "master"},
		},
		{
			testName:         "TestExcludeAntfarmPostfixVersions",
			givenVersions:    []string{"v1.0.0", "v1.1.1-antfarm", "v1.2.3-antfarm", "master"},
			excludeVersions:  []string{"v1.0.0", "v1.2.3"},
			expectedVersions: []string{"v1.1.1-antfarm", "master"},
		},
	}

	// Execute tests
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			excludeVersionsTest(t, tt)
		})
	}
}

// TestGetReleases checks if we get correct Sia releases from Sia Gitlab
// repository. Some official Sia releases need patches to work with Sia Antfarm
// so we need to build and test on patched version having '-antfarm' git tag
// suffix.
// NOTE: These patches are ONLY to enable the Sia Antfarm to run and are not
// intended to address any underlying bugs in siad.
func TestGetReleases(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	releases, err := GetReleases(minReleaseVersion)
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

// excludeVersionsTest is a testing helper for TestExcludeVersions test group.
// It tests excludeVersions function.
func excludeVersionsTest(t *testing.T, tc excludeVersionTestConfig) {
	gotVersions := ExcludeVersions(tc.givenVersions, tc.excludeVersions)
	got := fmt.Sprintf("%q", gotVersions)
	exp := fmt.Sprintf("%q", tc.expectedVersions)
	if got != exp {
		t.Errorf("expected %v, got %v", exp, got)
	}
}
