/*
Package ant provides an abstraction for the functionality of 'ants' in the
antfarm. Ants are Sia clients that have a myriad of user stories programmed as
their behavior and report their successfullness at each user store.
*/
package ant

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gitlab.com/NebulousLabs/Sia/node/api/client"
	"gitlab.com/NebulousLabs/errors"
)

// newSiad spawns a new siad process using os/exec and waits for the api to
// become available.  siadPath is the path to Siad, passed directly to
// exec.Command.  An error is returned if starting siad fails, otherwise a
// pointer to siad's os.Cmd object is returned.  The data directory `datadir`
// is passed as siad's `--sia-directory`.
func newSiad(siadPath string, datadir string, apiAddr string, rpcAddr string, hostAddr string, apiPassword string) (*exec.Cmd, error) {
	if err := checkSiadConstants(siadPath); err != nil {
		return nil, err
	}
	// create a logfile for Sia's stderr and stdout.
	logfile, err := os.Create(filepath.Join(datadir, "sia-output.log"))
	if err != nil {
		return nil, err
	}
	args := []string{"--modules=cgthmrw", "--no-bootstrap", "--sia-directory=" + datadir, "--api-addr=" + apiAddr, "--rpc-addr=" + rpcAddr, "--host-addr=" + hostAddr}
	if apiPassword == "" {
		args = append(args, "--authenticate-api=false")
	}
	cmd := exec.Command(siadPath, args...)
	cmd.Stderr = logfile
	cmd.Stdout = logfile
	if apiPassword != "" {
		cmd.Env = append(os.Environ(), "SIA_API_PASSWORD="+apiPassword)
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	if err := waitForAPI(apiAddr, cmd); err != nil {
		return nil, err
	}

	return cmd, nil
}

// checkSiadConstants runs `siad version` and verifies that the supplied siad
// is running the correct, dev, constants. Returns an error if the correct
// constants are not running, otherwise returns nil.
func checkSiadConstants(siadPath string) error {
	cmd := exec.Command(siadPath, "version")
	output, err := cmd.Output()
	if err != nil {
		return err
	}

	if !strings.Contains(string(output), "-dev") {
		return errors.New("supplied siad is not running required dev constants")
	}

	return nil
}

// stopSiad tries to stop the siad running at `apiAddr`, issuing a kill to its
// `process` after a timeout.
func stopSiad(apiAddr string, process *os.Process) {
	opts, err := client.DefaultOptions()
	if err != nil {
		panic(err)
	}
	opts.Address = apiAddr
	if err := client.New(opts).DaemonStopGet(); err != nil {
		process.Kill()
	}

	// wait for 120 seconds for siad to terminate, then issue a kill signal.
	done := make(chan error)
	go func() {
		_, err := process.Wait()
		done <- err
	}()
	select {
	case <-done:
	case <-time.After(120 * time.Second):
		process.Kill()
	}
}

// waitForAPI blocks until the Sia API at apiAddr becomes available.
// if siad returns while waiting for the api, return an error.
func waitForAPI(apiAddr string, siad *exec.Cmd) error {
	opts, err := client.DefaultOptions()
	if err != nil {
		return errors.AddContext(err, "unable to get client options")
	}
	opts.Address = apiAddr
	c := client.New(opts)

	exitchan := make(chan error)
	go func() {
		_, err := siad.Process.Wait()
		exitchan <- err
	}()

	// Wait for the Sia API to become available.
	success := false
	for start := time.Now(); time.Since(start) < 5*time.Minute; time.Sleep(time.Millisecond * 100) {
		if success {
			break
		}
		select {
		case err := <-exitchan:
			return fmt.Errorf("siad exited unexpectedly while waiting for api, exited with error: %v", err)
		default:
			if _, err := c.ConsensusGet(); err == nil {
				success = true
			}
		}
	}
	if !success {
		stopSiad(apiAddr, siad.Process)
		return errors.New("timeout: couldnt reach api after 5 minutes")
	}
	return nil
}
