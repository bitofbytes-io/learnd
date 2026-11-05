#!/usr/bin/env bash
# Exercises the staged templ-update allowlist against renames, non-regular
# index modes, deletions, generated templ output, and quoted paths.

set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
checker="${root}/test-support/check-staged-templ-update.sh"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

cd "${work}"
git init -q
git config user.email "templ-update-check@example.com"
git config user.name "templ-update-check"
mkdir -p .github/workflows internal/ui/pages
printf 'name: CI\n' > .github/workflows/ci.yml
printf 'module example\n\ngo 1.26.10\n' > go.mod
printf 'sum v1\n' > go.sum
printf 'package pages\n' > internal/ui/pages/login_templ.go
git add .
git commit -q -m init
base="$(git rev-parse HEAD)"

expect_reject() {
  local name="$1"
  if bash "${checker}"; then
    echo "expected rejection: ${name}" >&2
    exit 1
  fi
  git reset -q --hard "${base}"
  git clean -q -ffd
}

expect_allow() {
  local name="$1"
  if ! bash "${checker}"; then
    echo "expected allow: ${name}" >&2
    exit 1
  fi
  git reset -q --hard "${base}"
  git clean -q -ffd
}

# Renaming the workflow to a *_templ.go name is invisible to git apply --numstat.
git mv .github/workflows/ci.yml ok_templ.go
git diff --cached --binary > "${work}/rename.patch"
git reset -q --hard "${base}"
git apply --index --binary "${work}/rename.patch"
expect_reject "rename .github/workflows/ci.yml to ok_templ.go"

ln -sfn /tmp go.mod
git add -- go.mod
expect_reject "replace go.mod with a symlink"

ln -sfn /tmp ok_templ.go
git add -- ok_templ.go
expect_reject "add symlink named ok_templ.go"

git update-index --add --cacheinfo "160000,$(git rev-parse HEAD),go.mod"
expect_reject "replace go.mod with a gitlink"

rm go.sum
git add -- go.sum
expect_reject "delete go.sum"

rm go.mod
git add -- go.mod
expect_reject "delete go.mod"

rm internal/ui/pages/login_templ.go
git add -- internal/ui/pages/login_templ.go
expect_reject "delete *_templ.go"

printf 'package ui\n' > internal/ui/sneaky.go
git add -- internal/ui/sneaky.go
expect_reject "add a Go file that is not templ output"

python3 -c 'open("bad\nname", "w").write("x\n")'
git add -- $'bad\nname'
expect_reject "newline in the path"

python3 -c 'open("bad\tname", "w").write("x\n")'
git add -- $'bad\tname'
expect_reject "tab in the path"

python3 -c 'open("go.mod\"", "w").write("x\n")'
git add -- 'go.mod"'
expect_reject "quoted go.mod"

printf 'module example\n\ngo 1.26.10\n\nrequire example.com/dep v1.2.3\n' > go.mod
printf 'sum v2\n' > go.sum
printf 'package pages\n\nfunc Login() {}\n' > internal/ui/pages/login_templ.go
git add -- go.mod go.sum internal/ui/pages/login_templ.go
expect_allow "regular go.mod, go.sum, and *_templ.go modifications"

mkdir -p internal/ui/partials
printf 'package partials\n' > internal/ui/partials/entry_row_templ.go
git add -- internal/ui/partials/entry_row_templ.go
expect_allow "regular nested *_templ.go addition"

git rm -q -f go.sum
git commit -q -m 'drop go.sum'
printf 'sum v2\n' > go.sum
git add -- go.sum
expect_allow "regular go.sum addition"
git reset -q --hard "${base}"
git clean -q -ffd

echo "staged templ update allowlist passed"
