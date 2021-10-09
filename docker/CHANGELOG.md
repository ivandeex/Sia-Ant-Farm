# Sia Antfarm Docker Image Changelog

## Mar 12, 2021:
### v1.1.3
**Key Updates**
- Use Antfarm `dev` version in the docker image to use Sia `dev` constants and
  variables.
- Update Docker image to use Sia Antfarm `v1.1.3` which contains several
  Antfarm stability updates.
- Update build test to check that renter becomes upload ready within a timeout.

**Other**
- Refactor build test.

## Mar 4, 2021:
### v1.1.1
**Key Updates**
- Update Docker image to use Sia Antfarm `v1.1.1` which uses Sia `v1.5.5`.

## Feb 22, 2021:
### v1.1.0
**Key Updates**
- Update Docker image to use Sia Antfarm `v1.1.0` which uses Sia `v1.5.4`.

## Nov 18, 2020:
### v1.0.5
**Key Updates**
- Allow to publish all APIAddr ports by parsing config and setting socat port
  forwarding automatically.

## Nov 13, 2020:
### v1.0.4
**Key Updates**
- Update Docker image to use Sia Antfarm `v1.0.4` which uses Sia `v1.5.3`.

## Nov 11, 2020:
### v1.0.3
**Key Updates**
- Update Docker image to use Sia Antfarm `v1.0.3` which uses Sia `v1.5.2`.

## Nov 5, 2020:
### v1.0.2
**Key Updates**
- Update Docker image to use Sia Antfarm `v1.0.2` which uses Sia `v1.5.1`.

**Bugs Fixed**
- Update `antfarm-data` paths in `Dockefile`, `run.sh`, `build-test.sh` and
  `README.md` so that Sia Antfarm `v1.0.2` can successfully delete data
  directory on startup.

## Jun 1, 2020:
### v1.0.1
**Key Updates**
- Create Docker image to use Sia Antfarm `v1.0.1` which uses Sia `v1.5.0`.