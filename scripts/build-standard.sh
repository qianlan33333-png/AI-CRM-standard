#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
test -z "$(git status --porcelain --untracked-files=no)" || { echo 'Commit source before building a release'; exit 1; }
version=$(git rev-parse HEAD)
node scripts/materialize-donor-views.mjs --mode apply
node scripts/generate-ai-assistant-client.mjs
npm run build
node scripts/build-v3-host-adapters.mjs
docker build --platform linux/amd64 -f deploy/Dockerfile -t "crm-standard:$version" .
mkdir -p release
docker save --platform linux/amd64 "crm-standard:$version" postgres:16-bookworm caddy:2 | gzip > "release/crm-standard-$version.tar.gz"
shasum -a 256 "release/crm-standard-$version.tar.gz" > "release/crm-standard-$version.sha256"
echo "Built $version"
