#!/bin/bash
export XG2G_API_TOKEN=test
export XG2G_API_TOKEN_SCOPES="*"
export XG2G_ENIGMA2_BASEURL=http://127.0.0.1:8080
export XG2G_OWI_BASE=http://127.0.0.1:8080
export XG2G_STREAMING_POLICY=universal
# Disable Strict Readiness for 'Happy Path' simulation? 
# User asked for 'READY_STRICT=true' behavior check separately.
# Let's verify defaults first.
./xg2g > server.log 2>&1 &
echo $! > server.pid
