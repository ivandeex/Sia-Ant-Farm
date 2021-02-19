# sia-antfarm
[![Build Status](https://travis-ci.org/NebulousLabs/Sia-Ant-Farm.svg?branch=travis)](https://travis-ci.org/NebulousLabs/Sia)

sia-antfarm is a collection of utilities for performing complex, end-to-end
tests of the [Sia](https://gitlab.com/NebulousLabs/Sia) platform.  These test
are long-running and offer superior insight into high-level network behaviour
than Sia's existing automated testing suite.

# Docker

sia-antfarm is also available as a docker image `nebulouslabs/siaantfarm`:
* [GitHub repository](https://github.com/NebulousLabs/docker-sia-ant-farm)
* [Docker Hub](https://hub.docker.com/r/nebulouslabs/siaantfarm)

# Requirements

## Generic
- Go installed. Sia Antfarm is tested extensively to run successfully on Go
  `1.15` on Linux and should be running well also on MacOS.
- `$GOPATH/bin` should be added to the `$PATH` so that built siad binary can be
  found and executed.

## Version Test Requirements

Sia Antfarm is capable of building and testing different released and custom
versions of siad. Examples are in directories `foundation-test` (for Foundation
hardfork tests) and `version-test` (for basic version tests and for renter and
hosts upgrade tests).

Antfarm clones `Sia` repo if it doesn't already exist at
`$GOPATH/src/gitlab.com/NebulousLabs/Sia`. The local `Sia` repo should be in a
state that allows Antfarm to checkout different releases or custom branches,
i.e. all changes should be committed. If there are any uncommitted, unstashed
changes, they will be reset and lost.

# Install

```shell
go get -u gitlab.com/NebulousLabs/Sia-Ant-Farm/...
cd $GOPATH/src/gitlab.com/NebulousLabs/Sia-Ant-Farm
make dependencies && make
```

To install debug version of Sia Antfarm with debug logs enabled, execute:

```shell
make dependencies && make install-debug
```

If `siad` (at `$GOPATH/src/gitlab.com/NebulousLabs/Sia/cmd/siad`) is updated
and should be used with Antfarm, `make dependencies` needs to be rerun.

# Running a sia-antfarm

This repository contains one utility, `sia-antfarm`. `sia-antfarm` starts up
a number of `siad` instances, using jobs and configuration options parsed from
the input `config.json`. `sia-antfarm` takes a flag, `-config`, which is a path
to a JSON file defining the ants and their jobs. See `nebulous-configs/` for
some examples that we use to test Sia.

An example `config.json`:

`config.json:`
```json
{
	"antconfigs": 
	[ 
		{
			"jobs": [
				"gateway"
			]
		},
		{
			"jobs": [
				"gateway"
			]
		},
		{
			"jobs": [
				"gateway"
			]
		},
		{
			"jobs": [
				"gateway"
			]
		},
		{
			"apiaddr": "127.0.0.1:9980",
			"jobs": [
				"gateway",
				"miner"
			]
		}
	],
	"autoconnect": true
}
```

This `config.json` creates 5 ants, with four running the `gateway` job and one
running a `gateway` and a `miner` job.  If `HostAddr`, `APIAddr`, `RPCAddr`,
`SiamuxAddr`, or `SiamuxWsAddr` are not specified, they will be set to a random
port. If `autoconnect` is set to `false`, the ants will not automatically be
made peers of each other.

Note that if you have UPnP enabled on your router, the ants connect to each
other over the public Internet. If you do not have UPnP enabled on your router
and want the ants connect to each other over public Internet, you must
configure your system so that the ants' `RPCAddr` and `HostAddr` ports are
accessible from the Internet, i.e. to forward ports from your public IP. You
can run ant farm local IP range (then you do not need UPnP enabled router or
manual port forwarding) if you set `AllowHostLocalNetAddress` to `true`.

When you installed the Antfarm binary (see section Install) you can start the
Antfarm executing e.g. with one of our configs:

```shell
sia-antfarm -config nebulous-configs/basic-renter-host-5.json
```

or with debug logs on:

```shell
sia-antfarm-debug -config nebulous-configs/basic-renter-host-5.json
```

## Available configuration options:

```
{
	'ListenAddress': the listen address that the `sia-antfarm` API listens on
	'AntConfigs': an array of `AntConfig` objects, defining the ants to run on this antfarm
	'AutoConnect': a boolean which automatically bootstraps the antfarm if provided
	'ExternalFarms': an array of strings, where each string is the api address of an external antfarm to connect to.
	'WaitForSync': wait with all jobs until all ants are in sync, defaults to false.
}
```

`AntConfig`s have the following options:
```
{
	'APIAddr': the api address for the ant to listen on, by default an unused localhost: bind address will be used.
	'APIPassword': the password to be used for authenticating certain calls to the ant.
	'RPCAddr': the RPC address for the ant to listen on, by default an unused bind address will be used.
	'HostAddr': the Host address for the ant to listen on, by default an unused bind address will be used.
	'SiamuxAddr': the SiaMux address for the ant to listen on, by default an unused bind address will be used.
	'SiamuxWsAddr': the SiaMux websocket address for the ant to listen on, by default an unused bind address will be used.
	'AllowHostLocalNetAddress': if set to true allows hosts to announce on local network without needing UPnP enabled router or manual port forwarding from public IP.
	'RenterDisableIPViolationCheck': relevant only for renter, if set to true allows renter to rent on hosts on the same IP subnets by disabling the `IPViolationCheck` for the renter.
	'SiaDirectory': the data directory to use for this ant, by default a unique directory in `./antfarm-data` will be generated and used.
	'SiadPath': the path to the `siad` binary, by default the `siad-dev` in your path will be used.
	'Jobs': an array of jobs for this ant to run. available jobs include: ['miner', 'host', 'noAllowanceRenter', 'renter', 'autoRenter', 'gateway']. 'noAllowanceRenter' job starts the renter and waits for renter wallet to be filled, 'renter' job starts the renter, sets default allowance and waits till the renter is upload ready, it doesn't starts any renter's background activity. 'autoRenter' does the same as 'renter' job and then starts renter's periodic file uploads, downloads, and deletions.
	'DesiredCurrency': a minimum (integer) amount of SiaCoin that this Ant will attempt to maintain by mining currency. This is mutually exclusive with the `miner` job.
}
```

# License

The MIT License (MIT)

