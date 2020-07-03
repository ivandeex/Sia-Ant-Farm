Version Scheme
--------------
Sia Ant Farm uses the following versioning scheme, vX.X.X
 - First Digit signifies a major (compatibility breaking) release
 - Second Digit signifies a major (non compatibility breaking) release
 - Third Digit signifies a minor or patch release

Version History
---------------

Latest:

## Jul 3, 2020:
### v1.0.1
**Key Updates**
- Allow local IPs for hosts.
- Replace `UseExternalIPWithoutUPnP` with `AllowHostLocalNetAddress`.
- Set host's netAddress when using `AllowHostLocalNetAddress`
- Fix uploads.
- Build and use `siad-dev` as default.
- Add `WaitForSync` to config and enable waiting for ants to sync.

## Jun 23, 2020:
### v1.0.0
**Key Updates**
- Add changelog generator
- Update to Sia v1.4.11
- Create SiadConfig struct
- Add gitlab yml for CI/CD
- Add `UseExternalIPWithoutUPnP` AntConfig option

