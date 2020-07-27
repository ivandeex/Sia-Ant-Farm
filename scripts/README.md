# Scripts Readme

## Script: `build-sia-dev-binaries.sh`
### Description
The script creates `upgrade-binaries` Git ignored directory in root of
`Sia Ant Farm` repository directory, builds `siad-dev` binaries from `Sia`
repository for all released versions from `v1.3.7` on and then for latest Sia
`master` branch and stores the binaries in subdirectories in the created
directory.

The script gets all released Sia versions from `v1.3.7` on by calling
`get-versions.sh` script.
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
The script connects to the `Sia` repository and lists all released Sia versions
from `v1.3.7` on to the commandline standard output. It is intended to be
helper script for `build-sia-dev-binaries.sh` and for `Sia Ant Farm` Go code to
get upgrade path for version tests.
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
