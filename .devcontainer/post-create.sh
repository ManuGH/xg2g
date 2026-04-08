#!/usr/bin/env bash
set -euo pipefail

make install
make dev-tools
make doctor
