#!/bin/sh
set -eu
# Compare with HEAD so staged drift is rejected too.
git diff --exit-code HEAD -- '*_templ.go'
untracked=$(git ls-files --others --exclude-standard -- '*_templ.go')
if [ -n "$untracked" ]; then
    printf 'Untracked generated templates; regenerate and commit them:\n%s\n' "$untracked" >&2
    exit 1
fi
