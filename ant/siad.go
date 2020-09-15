/*
Package ant provides an abstraction for the functionality of 'ants' in the
antfarm. Ants are Sia clients that have a myriad of user stories programmed as
their behavior and report their successfullness at each user store.
*/
package ant

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gitlab.com/NebulousLabs/Sia/node/api/client"
	"gitlab.com/NebulousLabs/errors"
)

const (
	// stopSiadTimeout defines timeout for stopping siad process gracefully
	stopSiadTimeout = 120 * time.Second

	// waitForFullSetupFrequency defines how frequently to check if Sia daemon
	// finished full setup
	waitForFullSetupFrequency = time.Millisecond * 100

	// waitForFullSetupTimeout defines timeout for waiting for Sia daemon to
	// finish full setup
	waitForFullSetupTimeout = time.Second * 20
)

// SiadConfig contains the necessary config information to create a new siad
// instance
type SiadConfig struct {
	APIAddr                       string
	APIPassword                   string
	DataDir                       string
	HostAddr                      string
	RPCAddr                       string
	SiadPath                      string
	SiaMuxAddr                    string
	SiaMuxWsAddr                  string
	AllowHostLocalNetAddress      bool
	RenterDisableIPViolationCheck bool
}

// newSiad spawns a new siad process using os/exec and waits for the api to
// become available.  siadPath is the path to Siad, passed directly to
// exec.Command.  An error is returned if starting siad fails, otherwise a
// pointer to siad's os.Cmd object is returned.  The data directory `datadir`
// is passed as siad's `--sia-directory`.
func newSiad(config SiadConfig) (*exec.Cmd, error) {
	if err := checkSiadConstants(config.SiadPath); err != nil {
		return nil, errors.AddContext(err, "error with siad constants")
	}
	// Create a logfile for Sia's stderr and stdout.
	logFilename := "sia-output.log"
	logFilePath := filepath.Join(config.DataDir, logFilename)
	logfile, err := os.Create(logFilePath)
	if err != nil {
		return nil, errors.AddContext(err, "unable to create log file")
	}

	// Create siad config arguments
	args := []string{
		"--modules=cgthmrw",
		"--no-bootstrap",
		"--sia-directory=" + config.DataDir,
		"--api-addr=" + config.APIAddr,
		"--rpc-addr=" + config.RPCAddr,
		"--host-addr=" + config.HostAddr,
	}

	// Set siamux only if it is supported by given siad version
	siamuxSupported, err := siadFlagSupported(config.SiadPath, "--siamux-addr string")
	if err != nil {
		return nil, errors.AddContext(err, "can't determine siamux support")
	}
	if siamuxSupported {
		args = append(args, "--siamux-addr="+config.SiaMuxAddr)
	}

	// Set siamux WS only if it is supported by given siad version
	siamuxWSSupported, err := siadFlagSupported(config.SiadPath, "--siamux-addr-ws string")
	if err != nil {
		return nil, errors.AddContext(err, "can't determine siamux WS support")
	}
	if siamuxWSSupported {
		args = append(args, "--siamux-addr-ws="+config.SiaMuxWsAddr)
	}

	if config.APIPassword == "" {
		args = append(args, "--authenticate-api=false")
	}

	// Start siad, allow absolute and relative paths in config.SiadPath
	siadCommand := fmt.Sprintf("%v %v", config.SiadPath, strings.Join(args, " "))
	cmd := exec.Command("sh", "-c", siadCommand) //nolint:gosec
	cmd.Stderr = logfile
	cmd.Stdout = logfile
	if config.APIPassword != "" {
		cmd.Env = append(os.Environ(), "SIA_API_PASSWORD="+config.APIPassword)
	}

	if err := cmd.Start(); err != nil {
		return nil, errors.AddContext(err, "unable to start process")
	}

	// Wait until siad full setup is finished
	err = waitForFullSetup(config, cmd, logFilePath)
	if err != nil {
		return nil, errors.AddContext(err, "wait for siad full setup failed")
	}

	return cmd, nil
}

// checkSiadConstants runs `siad version` and verifies that the supplied siad
// is running the correct, dev, constants. Returns an error if the correct
// constants are not running, otherwise returns nil.
func checkSiadConstants(siadPath string) error {
	cmd := exec.Command(siadPath, "version") //nolint:gosec
	output, err := cmd.Output()
	if err != nil {
		return err
	}

	if !strings.Contains(string(output), "-dev") {
		return errors.New("supplied siad is not running required dev constants")
	}

	return nil
}

//siadFlagSupported determines if the given siad binary supports the given flag
func siadFlagSupported(siadPath, flag string) (bool, error) {
	siadHelpCommand := fmt.Sprintf("%v -h", siadPath)
	helpCmd := exec.Command("sh", "-c", siadHelpCommand) //nolint:gosec
	output, err := helpCmd.Output()
	if err != nil {
		return false, errors.AddContext(err, "unable to determine siad flag support")
	}
	outputStr := string(output)
	if strings.Contains(outputStr, flag) {
		return true, nil
	}
	return false, nil
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
	case <-time.After(stopSiadTimeout):
		process.Kill()
	}
}

// waitForFullSetup blocks until the Sia daemon finishes full setup. If siad
// returns while waiting for the api, return an error.
func waitForFullSetup(config SiadConfig, siad *exec.Cmd, logFilePath string) error {
	exitchan := make(chan error)
	go func() {
		_, err := siad.Process.Wait()
		exitchan <- err
	}()

	// Wait for siad full setup finished
	success := false
waitLoop:
	for start := time.Now(); time.Since(start) < waitForFullSetupTimeout; time.Sleep(waitForFullSetupFrequency) {
		select {
		case err := <-exitchan:
			msg := "siad exited unexpectedly while waiting for api"
			if err != nil {
				return fmt.Errorf(msg+", exited with error: %v", err)
			}
			return fmt.Errorf(msg)
		default:
			logContent, err := ioutil.ReadFile(logFilePath)
			if err != nil {
				log.Printf("[ERROR] [ant] [%v] Can't read %v: %v\n", config.DataDir, logFilePath, err)
			}
			if strings.Contains(string(logContent), "Finished full setup in") {
				success = true
				break waitLoop
			}
		}
	}
	if !success {
		stopSiad(config.APIAddr, siad.Process)
		return fmt.Errorf("siad hasn't finished full setup within %v timeout", waitForFullSetupTimeout)
	}
	return nil
}
