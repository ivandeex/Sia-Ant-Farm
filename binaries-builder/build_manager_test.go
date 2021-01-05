package binariesbuilder

import (
	"testing"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/test"
)

// TestBuildManager tests building binaries for multiple tests.
func TestBuildManager(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	// Prepare subtests.
	subtests := []struct {
		name     string
		versions []string
	}{
		{name: "TestBuildManagerA", versions: []string{"master"}},
		{name: "TestBuildManagerB", versions: []string{"v1.5.0", "v1.5.1", "master"}},
	}

	// Run subtests.
	for _, tt := range subtests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			// Subtests should be run in parallel.
			t.Parallel()

			// Prepare test logger.
			dataDir := test.TestDir(tt.name)
			testLogger := test.NewTestLogger(t, dataDir)
			defer func() {
				if err := testLogger.Close(); err != nil {
					t.Fatal(err)
				}
			}()

			// Start building the versions.
			err := StaticBuilder.BuildVersions(testLogger, BinariesDir, tt.versions...)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
