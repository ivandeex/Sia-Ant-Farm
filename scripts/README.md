# Scripts Readme

## Script: `build-sia-dev-binaries.sh`
### Description
The script creates `upgrade-binaries` Git ignored directory in root of
`Sia Ant Farm` repository directory, builds `siad-dev` binaries from `Sia`
repository for all released versions from `v1.3.7` on and then for latest Sia
`master` branch and stores the binaries as `siad-dev` in subdirectories with
format `Sia-{version}-{os}-{architecture}` in the created directory
`upgrade-binaries`.

The script gets all released Sia versions from `v1.3.7` on by calling
`get-versions.sh` script.
### Requirements
`Sia` git repository must be a sibling of `Sia-Ant-Farm` git repository on
local machine when this script is executed.
### Execution
Execute the script e.g. from the `Sia Ant Farm` repository root:
```
scripts/build-sia-dev-binaries.sh
```

or e.g. from scripts directory:
```
cd scripts
./build-sia-dev-binaries.sh
```
## Script: `get-versions.sh`
### Description
The script connects to the `Sia` repository and lists all released Sia
versions' git tag names (git tags) from `v1.3.7` on in ascending order to the
commandline standard output, each version (git tag) is printed on a separate
line.

It is intended to be helper script for `build-sia-dev-binaries.sh` and for
`Sia Ant Farm` Go code to get upgrade path for version tests.
### Requirements
* `jq` library installed in PATH
### Execution
Execute the script e.g. from the `Sia Ant Farm` repository root:
```
scripts/get-versions.sh
```

or e.g. from scripts directory:
```
cd scripts
./get-versions.sh
```
