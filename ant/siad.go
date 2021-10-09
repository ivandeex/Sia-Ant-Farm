/*
Package ant provides an abstraction for the functionality of 'ants' in the
antfarm. Ants are Sia clients that have a myriad of user stories programmed as
their behavior and report their successfullness at each user store.
*/
package ant

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.sia.tech/sia-antfarm/persist"
	"go.sia.tech/siad/modules"
	"go.sia.tech/siad/node/api/client"
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
func newSiad(logger *persist.Logger, config SiadConfig) (*exec.Cmd, error) {
	if err := checkSiadConstants(config.SiadPath); err != nil {
		return nil, errors.AddContext(err, "error with siad constants")
	}
	// Create a logfile for Sia's stderr and stdout.
	logFilename := "sia-output.log"
	logFilePath := filepath.Join(config.DataDir, logFilename)
	logfile, err := os.OpenFile(logFilePath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, modules.DefaultFilePerm)
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

	// Create multiwriter to append to log file and to buffer. In buffer we
	// don't see old logs and we check if current full setup finished.
	var buf bytes.Buffer
	mw := io.MultiWriter(logfile, &buf)

	// Start siad, allow absolute and relative paths in config.SiadPath
	siadCommand := fmt.Sprintf("%v %v", config.SiadPath, strings.Join(args, " "))
	cmd := exec.Command("sh", "-c", siadCommand) //nolint:gosec
	cmd.Stderr = mw
	cmd.Stdout = mw

	// After we are done waiting for full setup finished or an error
	// occurred, we don't need to write to the buffer anymore.
	defer func() {
		cmd.Stderr = logfile
		cmd.Stdin = logfile
	}()

	if config.APIPassword != "" {
		cmd.Env = append(os.Environ(), "SIA_API_PASSWORD="+config.APIPassword)
	}

	if err := cmd.Start(); err != nil {
		return nil, errors.AddContext(err, "unable to start process")
	}

	// Wait until siad full setup is finished
	err = waitForFullSetup(logger, config, cmd, &buf)
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
func stopSiad(logger *persist.Logger, dataDir string, apiAddr, apiPassword string, process *os.Process) {
	opts, err := client.DefaultOptions()
	if err != nil {
		panic(err)
	}
	opts.Address = apiAddr
	opts.Password = apiPassword
	if err := client.New(opts).DaemonStopGet(); err != nil {
		logger.Errorf("%v: can't stop siad daemon: %v", dataDir, err)
		if er := process.Kill(); er != nil {
			logger.Errorf("%v: can't kill siad process: %v", dataDir, er)
		}
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
		if err := process.Kill(); err != nil {
			logger.Errorf("%v: can't kill siad process: %v", dataDir, err)
		}
	}
}

// waitForFullSetup blocks until the Sia daemon finishes full setup. If siad
// terminates while waiting for full setup or a timeout occurs, returns an
// error. siadOutput expects to receive combined siad stdin and stderr output.
func waitForFullSetup(logger *persist.Logger, config SiadConfig, siad *exec.Cmd, siadOutput *bytes.Buffer) error {
	// Prepare channel if siad process terminates
	exitChan := make(chan error)
	go func() {
		_, err := siad.Process.Wait()
		exitChan <- err
	}()

	// Wait for siad full setup finished
	start := time.Now()
	var logContent string
	for {
		select {
		case err := <-exitChan:
			// Siad process terminated
			errMsg := errors.New("siad exited unexpectedly while waiting for full setup")
			return errors.Compose(errMsg, err)
		case <-time.After(waitForFullSetupFrequency):
		}

		// Timeout
		if time.Since(start) > waitForFullSetupTimeout {
			stopSiad(logger, config.DataDir, config.APIAddr, config.APIPassword, siad.Process)
			return fmt.Errorf("siad hasn't finished full setup within %v timeout", waitForFullSetupTimeout)
		}

		// Read siad output
		logContent += siadOutput.String()
		if strings.Contains(logContent, "Finished full setup in") {
			return nil
		}
	}
}
