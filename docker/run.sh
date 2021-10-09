#!/bin/sh

# We are using socat in order ant localhost http API to be accessible outside
# of docker container.
# Note: Default shell in Debian Slim is dash.

# Get internal docker container ip for port forwarding.
internal_ip_address=$(hostname -I)

# Trim white space.
IFS=' ' read internal_ip_address <<EOF
$internal_ip_address
EOF

# Find all APIAddr ports in config, extract ports, set socat forwarding between
# internal ip address and internal localhost ip address.
grep -i apiaddr $CONFIG | \
grep -oe '\([0-9]\{4,5\}\)' | \
xargs -n1 -I % sh -c "socat tcp-listen:%,bind=$internal_ip_address,reuseaddr,fork tcp:localhost:%,bind=127.0.0.1 &"

# Sia Antfarm deletes antfarm-data directory at startup, so this directory
# itself can't be mounted as a volume, but an intermediary directory can be.
cd data

# We are using `exec` to start Sia Ant Farm in order to ensure that it will be
# run as PID 1. We need that in order to have Sia Ant Farm receive OS signals
# (e.g. SIGTERM) on container shutdown, so it can exit gracefully.
exec sia-antfarm-dev -config=../$CONFIG