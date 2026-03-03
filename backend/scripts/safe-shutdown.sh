#!/bin/bash
# Safe Shutdown Script
# Targets only xg2g and its dev loop to prevent SSH disconnects.

echo "Initiating safe process termination..."

# 1. Stop the dev loop first
if pgrep -f "run_dev.sh" > /dev/null; then
    echo "Stopping run_dev.sh..."
    pkill -f "run_dev.sh"
fi

# 2. Stop the daemon
if pgrep -x "xg2g" > /dev/null; then
    echo "Stopping xg2g daemon..."
    pkill -x "xg2g"
fi

# 3. Clean up any containers
if docker ps -q --filter name=xg2g > /dev/null; then
    echo "Stopping xg2g docker containers..."
    docker stop "$(docker ps -q --filter name=xg2g)" > /dev/null 2>&1 || true
fi

echo "Safe shutdown complete."
