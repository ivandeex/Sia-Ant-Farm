package versiontest

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"gitlab.com/NebulousLabs/Sia/build"
	"gitlab.com/NebulousLabs/errors"
)

const (
	// siaRepoID is Gitlab Sia repository ID taken from:
	// https://gitlab.com/NebulousLabs/Sia > Project Overview > Details.
	siaRepoID = "7508674"

	// Build constants
	goos = "linux"
	arch = "amd64"
)

type (
	// bySemanticVersion is a type for implementing sort.Interface to sort by
	// semantic version.
	bySemanticVersion []string

	// command defines a struct for parameters to call execute method.
	command struct {
		// Specific environment variables to set
		envVars map[string]string
		// Name of the command
		name string
		// Command's subcommands or arguments
		args []string
	}
)

// buildSiad builds specified siad-dev versions defined by git tags into the
// given directory. If the given directory is relative path, it is relative to
// Sia-Ant-Farm/version-test directory.
func buildSiad(binariesDir string, versions ...string) error {
	vs := strings.Join(versions, ", ")
	msg := fmt.Sprintf("[INFO] [build-binaries] Preparing to build siad versions: %v", vs)
	log.Println(msg)

	// Get current working directory and change back to it when done
	startDir, err := os.Getwd()
	if err != nil {
		return errors.AddContext(err, "can't get current working directory")
	}
	defer os.Chdir(startDir)

	// Clone Sia repository if it doesn't exist locally
	goPath, ok := os.LookupEnv("GOPATH")
	if !ok {
		return errors.New("couldn't get GOPATH environment variable")
	}
	gitlabNebulous := "gitlab.com/NebulousLabs"
	gitlabSia := fmt.Sprintf("%v/Sia", gitlabNebulous)
	siaPath := fmt.Sprintf("%v/src/%v", goPath, gitlabSia)
	siaRepoURL := fmt.Sprintf("https://%v.git", gitlabSia)
	err = gitClone(siaRepoURL, siaPath)
	if err != nil {
		return errors.AddContext(err, "can't clone Sia repository")
	}

	// Set working dir to Sia repository
	err = os.Chdir(siaPath)
	if err != nil {
		return errors.AddContext(err, "can't change to Sia directory")
	}

	// Git reset to clean git repository
	cmd := command{
		name: "git",
		args: []string{"reset", "--hard", "HEAD"},
	}
	_, err = cmd.execute()
	if err != nil {
		return errors.AddContext(err, "can't reset Sia git repository")
	}

	// Git pull to get latest state
	cmd = command{
		name: "git",
		args: []string{"pull", "origin", "master"},
	}
	_, err = cmd.execute()
	if err != nil {
		return errors.AddContext(err, "can't pull Sia git repository")
	}

	for _, version := range versions {
		msg := fmt.Sprintf("[INFO] [build-binaries] Building a siad version: %v", version)
		log.Println(msg)

		// Create directory to store each version siad binary
		binarySubDir := fmt.Sprintf("Sia-%v-%v-%v", version, goos, arch)
		var binaryDir string
		if filepath.IsAbs(binariesDir) {
			binaryDir = filepath.Join(binariesDir, binarySubDir)
		} else {
			binaryDir = filepath.Join(startDir, binariesDir, binarySubDir)
		}

		err := os.MkdirAll(binaryDir, 0700)
		if err != nil {
			return errors.AddContext(err, "can't create a directory for storing built siad binary")
		}

		// Checkout merkletree repository correct commit in for Sia v1.4.0
		merkletreePath := filepath.Join(goPath, "src", gitlabNebulous, "merkletree")
		if version == "v1.4.0" {
			// Clone merkletree repo if not yet available
			gitlabMerkletree := fmt.Sprintf("%v/merkletree", gitlabNebulous)
			merkletreeRepoURL := fmt.Sprintf("https://%v.git", gitlabMerkletree)
			err := gitClone(merkletreeRepoURL, merkletreePath)
			if err != nil {
				return errors.AddContext(err, "can't clone merkletree repository")
			}

			// Checkout the specific merkletree commit
			err = gitCheckout(merkletreePath, "bc4a11e")
			if err != nil {
				return errors.AddContext(err, "can't checkout specific merkletree commit")
			}
		}

		// Checkout the version
		err = gitCheckout(siaPath, version)
		if err != nil {
			return errors.AddContext(err, "can't checkout specific Sia version")
		}

		// Get dependencies
		cmd = command{
			name: "go",
			args: []string{"get", "-d", "./..."},
		}
		_, err = cmd.execute()
		if err != nil {
			return errors.AddContext(err, "can't get dependencies")
		}

		// Compile siad-dev binaries
		pkg := "gitlab.com/NebulousLabs/Sia/cmd/siad"
		binaryName := "siad-dev"

		// Set ldflags according to Sia/Makefile
		cmd = command{name: "date"}
		buildTime, err := cmd.execute()
		if err != nil {
			return errors.AddContext(err, "can't get build time")
		}
		buildTime = strings.TrimSpace(buildTime)
		cmd = command{name: "git", args: []string{"rev-parse", "--short", "HEAD"}}
		gitRevision, err := cmd.execute()
		if err != nil {
			return errors.AddContext(err, "can't get git revision")
		}
		gitRevision = strings.TrimSpace(gitRevision)

		var ldFlags string
		ldFlags += fmt.Sprintf(" -X gitlab.com/NebulousLabs/Sia/build.GitRevision=%v", gitRevision)
		ldFlags += fmt.Sprintf(" -X gitlab.com/NebulousLabs/Sia/build.BuildTime='%v'", buildTime)

		var args []string
		args = append(args, "build")
		args = append(args, "-a")
		args = append(args, "-tags='dev debug profile netgo'")
		args = append(args, "-trimpath")
		args = append(args, "-ldflags")
		args = append(args, "\""+ldFlags+"\"")
		args = append(args, "-o")
		args = append(args, filepath.Join(binaryDir, binaryName))
		args = append(args, pkg)

		cmd = command{
			envVars: map[string]string{
				"GOOS":   goos,
				"GOARCH": arch,
			},
			name: "go",
			args: args,
		}
		_, err = cmd.execute()
		if err != nil {
			return errors.AddContext(err, "can't build siad binary")
		}

		// Checkout merkletree repository back to master after Sia v1.4.0
		if version == "v1.4.0" {
			err := gitCheckout(merkletreePath, "master")
			if err != nil {
				return errors.AddContext(err, "can't checkout merkletree master")
			}
		}
	}

	return nil
}

// getReleases returns slice of git tags of Sia Gitlab releases greater than or
// equal to the given minimal version in ascending semantic version order
func getReleases(minVersion string) ([]string, error) {
	// Get releases from Gitlab Sia repository
	url := fmt.Sprintf("https://gitlab.com/api/v4/projects/%v/releases", siaRepoID)
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return nil, errors.AddContext(err, "can't get releases from Gitlab")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("response status from Gitlab is not '200 OK' but %v", resp.Status)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.AddContext(err, "can't read response body")
	}

	// Decode response into slice of release data
	var releases []map[string]interface{}
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, errors.AddContext(err, "can't decode response from Gitlab")
	}

	// Collect release tags greater than or equal to minVersion
	var releaseTags []string
	for _, r := range releases {
		tag := fmt.Sprintf("%v", r["tag_name"])
		if build.VersionCmp(tag, minVersion) >= 0 {
			releaseTags = append(releaseTags, tag)
		}
	}

	// Sort releases in ascending order by semantic version
	sort.Sort(bySemanticVersion(releaseTags))

	return releaseTags, nil
}

// gitCheckout changes working directory to the git repository, performs git
// reset and git checkout by branch, tag or commit id specified in checkoutStr
// and changes working directory back to original directory.
func gitCheckout(gitRepoPath, checkoutStr string) error {
	// Get current working directory and change back to it when done
	startDir, err := os.Getwd()
	if err != nil {
		return errors.AddContext(err, "can't get current working directory")
	}
	defer os.Chdir(startDir)

	// Change working directory to the git repository
	err = os.Chdir(gitRepoPath)
	if err != nil {
		return errors.AddContext(err, "can't change to merkletree directory")
	}

	// Reset git
	cmd := command{
		name: "git",
		args: []string{"reset", "--hard", "HEAD"},
	}
	_, err = cmd.execute()
	if err != nil {
		return errors.AddContext(err, "can't reset git repository")
	}

	// Git checkout by branch, tag or commit id
	cmd = command{
		name: "git",
		args: []string{"checkout", checkoutStr},
	}
	_, err = cmd.execute()
	if err != nil {
		return errors.AddContext(err, "can't perform git checkout")
	}

	return nil
}

// gitClone clones git repository by given URL to the given path.
func gitClone(repoURL, repoPath string) error {
	// Return if directory already exists
	_, err := os.Stat(repoPath)
	if err != nil && !os.IsNotExist(err) {
		return errors.AddContext(err, "can't get directory info")
	} else if err == nil {
		return nil
	}

	// Directory doesn't exist
	log.Printf("[INFO] [build-binaries] Cloning git repository %v to %v.\n", repoURL, repoPath)

	// Create repository directory
	err = os.MkdirAll(repoPath, 0700)
	if err != nil {
		return errors.AddContext(err, "can't create repository directory")
	}

	// Clone repository
	cmd := command{
		name: "git",
		args: []string{"clone", repoURL, repoPath},
	}
	_, err = cmd.execute()
	if err != nil {
		return errors.AddContext(err, "can't clone repository")
	}

	return nil
}

// Len implements sort.Interface to sort by semantic version
func (s bySemanticVersion) Len() int {
	return len(s)
}

// Swap implements sort.Interface to sort by semantic version
func (s bySemanticVersion) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

// Less implements sort.Interface to sort by semantic version
func (s bySemanticVersion) Less(i, j int) bool {
	return build.VersionCmp(s[i], s[j]) < 0
}

// execute executes a given shell command defined by command receiver.
// Command struct is used instead of passing the whole command as a string and
// parsing string arguments because parsing arguments containing spaces would
// make the parsing much complex.
func (c command) execute() (string, error) {
	cmd := exec.Command(c.name, c.args...) //nolint:gosec
	cmd.Env = os.Environ()
	var envVars = []string{}
	for k, v := range c.envVars {
		ev := fmt.Sprintf("%v=%v", k, v)
		envVars = append(envVars, ev)
		cmd.Env = append(cmd.Env, ev)
	}

	out, err := cmd.CombinedOutput()

	if err != nil {
		readableEnvVars := strings.Join(envVars, " ")
		readableArgs := strings.Join(c.args, " ")
		readableCommand := fmt.Sprintf("%v %v %v", readableEnvVars, c.name, readableArgs)
		wd, err := os.Getwd()
		if err != nil {
			return "", errors.AddContext(err, "can't get working directory")
		}

		log.Printf("[ERROR] [build-binaries] Error executing bash command:\nWorking directory: %v\nCommand: %v\nOutput:\n%v\n", wd, readableCommand, string(out))

		msg := fmt.Sprintf("can't execute comand: %v", readableCommand)
		return "", errors.AddContext(err, msg)
	}
	return string(out), nil
}
