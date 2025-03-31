#!/bin/bash
# exit when any command fails
set -e
# get gitref
gitref=$(git rev-parse --short HEAD)
# create buildx builder
docker buildx create --name multi-builder | true
# use buildx builder
docker buildx use multi-builder
# build and push
docker buildx build --platform linux/amd64,linux/arm64 -t us-east4-docker.pkg.dev/production-911/docker-private/janitor:$gitref --push .
