#!/usr/bin/env bash
set -euo pipefail
host=${AETHER_DEPLOY_HOST:-aether@137.184.108.59}
key=${AETHER_DEPLOY_KEY:-$HOME/.ssh/voodoo}
ssh_opts=(-i "$key" -o BatchMode=yes)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/aether-linux ./cmd/aether
COPYFILE_DISABLE=1 tar --exclude node_modules --no-xattrs -czf bin/daemon.tgz -C packages daemon
scp "${ssh_opts[@]}" bin/aether-linux "$host:/tmp/aether"
scp "${ssh_opts[@]}" bin/daemon.tgz "$host:/tmp/daemon.tgz"
ssh "${ssh_opts[@]}" "$host" 'set -e
sudo install -m 755 /tmp/aether /opt/aether/bin/aether
tar -xzf /tmp/daemon.tgz -C /opt/aether/packages
cd /opt/aether/packages/daemon && bun install --silent
sudo systemctl restart aether-serve aether-connect
sleep 2
systemctl is-active aether-serve aether-connect'
