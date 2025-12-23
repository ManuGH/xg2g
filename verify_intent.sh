#!/bin/bash
set -e

echo "Creating session..."
# Using 'profile' instead of 'profileId'
curl -s -X POST -H "Content-Type: application/json" -d '{"serviceRef": "1:0:1:283D:3FB:1:C00000:0:0:0:", "profile": "pf1"}' http://localhost:8080/api/v3/intents
echo ""

echo "Waiting for session initialization..."
sleep 2

echo "Checking active sessions..."
curl -s http://localhost:8080/api/v3/sessions
echo ""
