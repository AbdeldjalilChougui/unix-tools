#!/bin/sh
# Runs the real tree(1) from a container, for machines without it:
#
#   TREE_BIN=$PWD/scripts/tree-docker.sh go test ./... [-update]
#
# The image is built on first use.
set -e
img=unix-tools-tree:2.2.1
if ! docker image inspect "$img" >/dev/null 2>&1; then
	docker build -q -t "$img" - >/dev/null 2>&1 <<'DOCKERFILE'
FROM debian:trixie-slim
RUN apt-get update && apt-get install -y --no-install-recommends tree && rm -rf /var/lib/apt/lists/*
DOCKERFILE
fi
exec docker run --rm -i -e LC_ALL -v "$PWD":/w -w /w "$img" tree "$@"
