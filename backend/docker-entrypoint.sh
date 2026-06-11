#!/bin/sh
set -eu

# Named volumes keep ownership from their first initialization. Older local
# api-data volumes may be root-owned, which prevents the non-root app user from
# persisting Twitch tokens and retained overlay state. Repair ownership on boot.
if [ "$(id -u)" = "0" ]; then
  mkdir -p /data
  chown -R overlaid:overlaid /data
  exec su-exec overlaid:overlaid "$@"
fi

exec "$@"
