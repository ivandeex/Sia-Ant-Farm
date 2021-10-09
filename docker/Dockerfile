FROM docker.io/golang:latest as antfarm

COPY . /home
WORKDIR /home
RUN make install-dev

#FROM ghcr.io/siafoundation/siad:master as siad

FROM debian:10.4-slim

# Install libraries
# Antfarm requires 'ss' utility which is part of 'iproute2'
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
            curl iproute2 socat socat \
 && rm -rf /var/lib/apt/lists

# Create antfarm and data dirs
RUN mkdir -p sia-antfarm/data

# Download sia-antfarm and siad-dev binaries
WORKDIR /sia-antfarm
COPY --from=antfarm /go/bin/sia-antfarm-dev .
COPY --from=antfarm /go/bin/siad-dev .
#COPY --from=siad /siad ./siad-dev

# Copy default config
ARG DIR ./docker
ENV CONFIG=config/basic-renter-5-hosts-docker.json
COPY ${DIR}/${CONFIG} config/

# Set path for sia-antfarm and siad-dev binaries
ENV PATH=/sia-antfarm:$PATH

# Start Ant Farm
COPY ${DIR}/run.sh .
ENTRYPOINT ["./run.sh"]
