Version Scheme
--------------
Sia Ant Farm uses the following versioning scheme, vX.X.X
 - First Digit signifies a major (compatibility breaking) release
 - Second Digit signifies a major (non compatibility breaking) release
 - Third Digit signifies a minor or patch release

Version History
---------------

Latest:

## Nov 10, 2020:
### v1.0.3
**Key Updates**
- Update Sia to the latest released version `v1.5.2`.

## Nov 3, 2020:
### v1.0.2
**Key Updates**
- Updated building siad binaries for version tests. When for a Sia release
  (e.g. `v1.4.8`) a git tag with `-antfarm` suffix exists (i.e.
  `v1.4.8-antfarm`), version test doesn't build Sia release, but it's patched
  version.
- Fix setting ant external IP addresses. This fixes version tests for testing
  from Sia version `v1.4.10` on.
- Write antfarm execution logs and tests logs to file instead of to console.
- Repost host announcement and host accepting contracts when the transactions
  were dropped and the renter doesn't see the host(s) as active.
- Do not overwrite `sia-output.log` on siad upgrades.
- Enable upgrading/downgrading renter ant's siad binary. Renter if fully
  functional after upgrade/downgrade.
- Update Sia to the latest released version `v1.5.1`.
- Update version tests to use the latest release as a base version (instead of
  using the latest master).

**Bugs Fixed**
- Fix closing ants' siad processes via API.
- Fix closing siad process in TestNewSiad.
- Fix stale upload at 0% occurring occasionally after renter's siad process
  restart or update.

**Other**
- Restructure Antfarm packages.
- Fix cloning Sia repository for building binaries when `vX.X.X-antfarm` tag
  was updated.
- Fix closing ants when error during starting antfarm occurs.
- Speedup closing antfarm by closing ants concurrently.
- Disable UPnP router discovery and clearing ports via UPnP on Gitlab CI.
- Enable `errcheck` linter and fix all `errcheck` issues in the repository.
- Add make option to install `sia-antfarm-debug` with debug messages printed to
  the log.
- Simplify `WaitForRenterUploadReady` to be easily used by tests.
- Define renter phases to be used in renter job to support alternative renter
  configs.

## Aug 10, 2020:
### v1.0.1
**Key Updates**
- Allow local IPs for hosts.
- Replace `UseExternalIPWithoutUPnP` with `AllowHostLocalNetAddress`.
- Set host's netAddress when using `AllowHostLocalNetAddress`
- Allow renter to rent on hosts on the same IP subnets (add config option
  `RenterDisableIPViolationCheck` and set `checkforipviolation` to `false`)
- Fix renter job thread groups.
- Fix uploads.
- Split existing `renter` job between basic `renter` and continuous
  `autorenter`.
- Update Sia to `v1.5.0`.
- Build and use `siad-dev` as default.
- Add `WaitForSync` to config and enable waiting for ants to sync.

**Bugs Fixed**
- Fix various loops that had rapid cycling or early exits. Cleaned up constants associated with the loops.

**Other**
- Add error checking to jobrunners' thread group Add() for antfarm closing to
  be faster.
- Added script to generate `siad-dev` binaries.
- Fix unique ant names check.

## Jun 23, 2020:
### v1.0.0
**Key Updates**
- Add changelog generator
- Update to Sia v1.4.11
- Create SiadConfig struct
- Add gitlab yml for CI/CD
- Add `UseExternalIPWithoutUPnP` AntConfig option

