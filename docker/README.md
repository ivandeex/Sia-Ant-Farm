# Docker Sia Ant Farm

[![Build Status](https://travis-ci.org/NebulousLabs/docker-sia-ant-farm.svg?branch=master)](https://travis-ci.org/NebulousLabs/docker-sia-ant-farm) 
[![Docker Pulls](https://img.shields.io/docker/pulls/nebulouslabs/siaantfarm.svg?maxAge=604800)](https://hub.docker.com/r/nebulouslabs/siaantfarm) 
[![License](http://img.shields.io/:license-mit-blue.svg)](LICENSE)

[Sia Ant Farm](https://gitlab.com/NebulousLabs/Sia-Ant-Farm) in a Docker container.

## Supported Tags

### Latest
* **latest**

### 1.1.3
* Sia AntFarm `v1.1.3` with AntFarm stability updates and updated build test

### 1.1.1
* Sia AntFarm `v1.1.1` based on Sia `v1.5.5`

### 1.1.0
* Sia AntFarm `v1.1.0` based on Sia `v1.5.4`

### 1.0.5
* Allows publishing multiple ant HTTP API ports

### 1.0.4
* Sia Ant Farm `v1.0.4` based on Sia `v1.5.3`

### 1.0.3
* Sia Ant Farm `v1.0.3` based on Sia `v1.5.2`

### 1.0.2
* Sia Ant Farm `v1.0.2` based on Sia `v1.5.1`

### 1.0.1
* Sia Ant Farm `v1.0.1` based on Sia `v1.5.0`

## Running Ant Farm in Docker container

### Basic Usage
To start Ant Farm with default configuration
(`config/basic-renter-5-hosts-docker.json`) execute:
```
docker run \
    --publish 127.0.0.1:9980:9980 \
    nebulouslabs/siaantfarm
```
Port `127.0.0.1:9980` above is the renter API address which you can use to
issue commands to the renter. For security reasons you should bind the port to
localhost (see `127.0.0.1:9980` above).

### Custom Configuration
By default the Sia Ant Farm docker image has a copy of
`config/basic-renter-5-hosts-docker.json` configuration file.

If you want to execute Ant Farm with a custom configuration, create your custom
configuration e.g. `config/custom-config.json`, mount your config directory and
set `CONFIG` environment variable to your custom configuration by executing:
```
docker run \
    --publish 127.0.0.1:9980:9980 \
    --volume $(pwd)/config:/sia-antfarm/config \
    --env CONFIG=config/custom-config.json \
    nebulouslabs/siaantfarm
```

### Change Port
To change port on which you can access the renter (e.g. to 39980) execute:
```
docker run \
    --publish 127.0.0.1:39980:9980 \
    nebulouslabs/siaantfarm
```

### Open multiple ports
In default configuration only renter's HTTP API port is accessible from outside
of the docker container. If you want to configure access to more or all the
ants, you need to use custom configuration file (described above) and each
ant's HTTP API port needs to be set in 2 places:
* In the configuration file
* Pubished when starting docker container

#### Specify port in configuration file
`APIAddr` setting needs to be set in the configuration file same way as it is
set for renter ant in default configuration file
`config/basic-renter-5-hosts-docker.json`. Hostname part can only have one of
two values: `127.0.0.1` or `localhost`.

Example snippet:
```
        ...
		{
			"AllowHostLocalNetAddress": true,
            "APIAddr": "127.0.0.1:10980",
			"Name": "host1",
			"Jobs": [
				"host"
			],
			"DesiredCurrency": 100000
		},
        ...
```

#### Publish port when starting docker
Once you have prepared configuration file, you can start the container. You
need to set the path to custom configuration via `CONFIG` environment variable
and publish each port via `--publish` flag.

Example docker run command:
```
docker run \
    --publish 127.0.0.1:9980:9980 \
    --publish 127.0.0.1:10980:10980 \
    --volume $(pwd)/config:/sia-antfarm/config \
    --env CONFIG=config/custom-config.json \
    nebulouslabs/siaantfarm
```

### Persistent Ant Farm Data
There are several ways how to persist Ant Farm data. To store `antfarm-data` in
the current directory can be done the following way:
```
docker run \
    --publish 127.0.0.1:9980:9980 \
    --volume $(pwd):/sia-antfarm/data \
    nebulouslabs/siaantfarm
```
