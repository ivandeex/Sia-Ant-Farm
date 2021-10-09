#!/bin/bash

## Exit immediately on error.
set -e

# Set function to be called on exit
trap 'exit_err_cleanup $?' EXIT

###############################################################################
# exit_err_cleanup is called at exit, if there was an error it calls container
# cleanup and reports an error.
# Globals: none
# Parameters:
# - $1: Script exit code
###############################################################################
exit_err_cleanup() {
  # Cleanup containers and report an error if any happened
  if [ "$1" != "0" ]; then
    cleanup_test_containers
    echo "ERROR: Error $1 occured"
  else
    echo "SUCCESS: Build and test finished successfully"
  fi

  exit $1
}

###############################################################################
# cleanup_test_containers logs massages and calls the the subcall to remove
# test containers.
# Globals: none
# Parameters: none
###############################################################################
cleanup_test_containers() {
  log_cmd "Removing test containers" cleanup_test_containers_subcall
}

###############################################################################
# cleanup_test_containers_subcall removes test containers without logs.
# Globals: none
# Parameters: none
###############################################################################
cleanup_test_containers_subcall() {
  CONTAINERS=$(docker ps -a -q --filter "name=sia-ant-farm-test-container")
  if [ ! -z $CONTAINERS ]; then
    docker stop $CONTAINERS
    docker rm   $CONTAINERS
  fi
}

###############################################################################
# log_cmd echoes the starting message, executes the command and echoes the
# success message.
# Globals: none
# Parameters:
# - $1: Log message
# - the rest: Command with parameters to be executed
###############################################################################
log_cmd() {
  # Get and echo log message
	local MSG=$1
	echo "Starting: $MSG..."

  # Execute the command
  shift 1
	"$@"

  # Log success
	echo "Success: $MSG"
}

###############################################################################
# wait_for_consensus waits for the ant consensus to be reachable.
# Globals: none
# Parameters:
# - $1: Timeout to wait for the consensus
# - $2: API port of the ant to be checked
###############################################################################
wait_for_consensus() {
  # Parameters
  local timeout=$1
  local port=$2
  
  # Set variables
  local url=http://localhost:$port/consensus
  local cmd="until wget --user-agent='Sia-Agent' -q -O - $url
    do
      sleep 1
    done"
  
  # Execute
  log_cmd "Waiting for consensus at $url" timeout $timeout sh -c "$cmd"
}

###############################################################################
# wait_for_renter_upload_ready waits until the renter ant is upload ready.
# Globals: none
# Parameters:
# - $1: Timeout to wait for the renter to become upload ready
# - $2: API port of the renter ant to be checked
###############################################################################
wait_for_renter_upload_ready() {
  # Parameters
  local timeout=$1
  local port=$2
  
  # Set variables
  local url=http://localhost:$port/renter/uploadready
  local cmd="until wget --user-agent='Sia-Agent' -q -O - $url | grep '\"ready\":true' 
    do
      sleep 1
    done"
  
  # Execute
  log_cmd "Waiting for renter to become upload ready at $url" timeout $timeout sh -c "$cmd"
}

# Remove test containers before we start
cleanup_test_containers

# Iterate over all Dockerfiles
for DIR in .
do
  # Build the image
  log_cmd "Building a test image according to $DIR/Dockerfile" docker build \
    --no-cache \
    --tag sia-ant-farm-image-test \
    -f $DIR/Dockerfile \
    .

  # Test with a single published standard renter port
  # Run container in detached state
  DUMMY_DATA_DIR=$(mktemp -d)
  TEST1_RENTER_PORT=9980
  
  log_cmd "Test 1: Starting test container" docker run \
    --detach \
    --publish 127.0.0.1:$TEST1_RENTER_PORT:9980 \
    --volume "${DUMMY_DATA_DIR}:/sia-antfarm/data" \
    --name sia-ant-farm-test-container \
    sia-ant-farm-image-test

  # Wait till API (/consensus) is accessible
  wait_for_consensus 120 $TEST1_RENTER_PORT

  # Wait for renter to become upload ready
  wait_for_renter_upload_ready 300 $TEST1_RENTER_PORT

  # Remove test container
  cleanup_test_containers

  # Test with 2 published ports:
  # A non-standard renter port and a host port
  # Run container in detached state

  DUMMY_DATA_DIR=$(mktemp -d)
  TEST2_RENTER_PORT=33333
  TEST2_HOST_PORT=34444
  
  log_cmd "Test 2: Starting test container" docker run \
    --detach \
    --publish 127.0.0.1:$TEST2_RENTER_PORT:9980 \
    --publish 127.0.0.1:$TEST2_HOST_PORT:10980 \
    --volume "${DUMMY_DATA_DIR}:/sia-antfarm/data" \
    --volume "$(pwd)/config:/sia-antfarm/config" \
    --env CONFIG=config/basic-renter-5-hosts-2-api-ports-docker.json \
    --name sia-ant-farm-test-container \
    sia-ant-farm-image-test
  
  # Wait till both APIs (/consensus) are accessible
  wait_for_consensus 120 $TEST2_RENTER_PORT
  wait_for_consensus 10 $TEST2_HOST_PORT
  
  # Wait for renter to become upload ready
  wait_for_renter_upload_ready 300 $TEST2_RENTER_PORT

  # Remove test container
  cleanup_test_containers
done
