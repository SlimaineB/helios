#!/bin/sh
# Entrypoint: wait for the bootstrap service to drop our config file.
set -e
config="${CONFIG_FILE:-/etc/helios/agent.yaml}"
i=0
while [ ! -f "$config" ]; do
  i=$((i + 1))
  if [ "$i" -gt 60 ]; then
    echo "timed out waiting for $config" >&2
    exit 1
  fi
  sleep 2
done
exec /usr/local/bin/helios-agent -config "$config"