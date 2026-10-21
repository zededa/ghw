#!/bin/sh
#
# Use and distribution licensed under the Apache license version 2.
#
# See the COPYING file in the root project directory for full text.
#
# Renames the Go module from github.com/jaypipes/ghw to github.com/zededa/ghw.
#
# zededa/ghw keeps one branch per upstream release, eve/<tag>: the upstream tag,
# EVE's patches on top of it, and this rename as the last commit. Every patch
# below the rename keeps upstream's module path, so it applies to upstream as it
# is. After rebasing the patches onto a new tag, drop the old rename commit, run
# this script and commit its output together with the script.
set -eu

cd "$(dirname "$0")/.."
git ls-files -z -- '*.go' 'go.mod' '*/go.mod' 'Dockerfile' |
	xargs -0 sed -i -e 's#github.com/jaypipes/ghw#github.com/zededa/ghw#g' --
# The new path sorts differently among the imports.
git ls-files -z -- '*.go' | xargs -0 gofmt -w --
