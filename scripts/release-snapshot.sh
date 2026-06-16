#!/usr/bin/env sh
set -eu

goreleaser release --snapshot --clean
