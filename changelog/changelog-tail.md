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

