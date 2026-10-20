#!/bin/sh
set -eu
# Include new Go files, while leaving templ-generated output to its generator.
unformatted=$(git ls-files --cached --others --exclude-standard -z -- '*.go' ':!:*_templ.go' | xargs -0 gofmt -l)
if [ -n "$unformatted" ]; then
    printf 'Run gofmt on:\n%s\n' "$unformatted" >&2
    exit 1
fi
