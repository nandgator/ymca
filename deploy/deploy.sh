#!/usr/bin/env bash
# Run this from the EC2 instance, inside deploy/, after `git pull` (or
# re-uploading the repo) picks up new code. Safe to re-run — `up -d --build`
# only rebuilds/restarts what changed.
set -euo pipefail
cd "$(dirname "$0")"

if [ ! -f .env.prod ]; then
  echo "Missing .env.prod — copy .env.prod.example to .env.prod and fill in real values first." >&2
  exit 1
fi

docker compose -f docker-compose.prod.yml --env-file .env.prod up -d --build
echo
echo "Status:"
docker compose -f docker-compose.prod.yml --env-file .env.prod ps
