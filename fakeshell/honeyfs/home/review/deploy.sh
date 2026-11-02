#!/bin/sh
# Deploy the demo app. On this demo machine scripts are shown, not run.
set -eu
make -C app build
systemctl --user restart demo-app
curl -fsS http://localhost:8080/healthz
