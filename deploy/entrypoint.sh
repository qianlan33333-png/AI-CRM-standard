#!/bin/sh
set -eu
if [ "${1:-}" = "migrate" ]; then
  migrate-river
  exec migrate-platform -dir /app/migrations -timeout 5m
fi
exec "$@"
