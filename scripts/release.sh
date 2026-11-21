#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'release: %s\n' "$*" >&2
  exit 1
}

mode=${1:-}
case "$mode" in
  patch|minor|major) ;;
  *) fail "usage: bash scripts/release.sh {patch|minor|major} [--dry-run]" ;;
esac
shift
dry_run=false
if [[ ${1:-} == --dry-run ]]; then
  dry_run=true
  shift
fi
[[ $# == 0 ]] || fail "unexpected argument: $1"

root=$(git rev-parse --show-toplevel)
cd "$root"

assert_clean_main() {
  local branch
  branch=$(git symbolic-ref --quiet --short HEAD || true)
  [[ $branch == main ]] || fail "run releases from main"
  [[ -z $(git status --porcelain) ]] || fail "working tree must be clean; commit and push changes first"
}

fetch_main() {
  GIT_TERMINAL_PROMPT=0 git fetch --quiet origin main --tags
  [[ $(git rev-parse HEAD) == $(git rev-parse refs/remotes/origin/main) ]] ||
    fail "main must match origin/main; push local commits or update main first"
}

latest_version() {
  local tag
  while IFS= read -r tag; do
    if [[ $tag =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
      printf '%s\n' "$tag"
      return
    fi
  done < <(git tag --list 'v[0-9]*' --sort=-version:refname)
}

assert_clean_main
fetch_main
commit=$(git rev-parse HEAD)
latest=$(latest_version)
if [[ -z $latest ]]; then
  next=v0.1.0
else
  remote_tag=$(GIT_TERMINAL_PROMPT=0 git ls-remote --tags origin "refs/tags/$latest")
  [[ -n $remote_tag ]] || fail "$latest has not been pushed; resolve the local tag before making another release"
  [[ $latest =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]
  major=${BASH_REMATCH[1]}
  minor=${BASH_REMATCH[2]}
  patch=${BASH_REMATCH[3]}
  case "$mode" in
    patch) patch=$((patch + 1)) ;;
    minor) minor=$((minor + 1)); patch=0 ;;
    major) major=$((major + 1)); minor=0; patch=0 ;;
  esac
  next="v$major.$minor.$patch"
fi
printf 'Next release: %s\n' "$next"

task check
task release:check

# Checks take time. Refuse to publish if the tree or remote changed meanwhile.
assert_clean_main
[[ $(git rev-parse HEAD) == "$commit" ]] || fail "HEAD changed during validation; run the command again"
fetch_main
[[ $(latest_version) == "$latest" ]] || fail "release tags changed during validation; run the command again"

if $dry_run; then
  printf 'Dry run complete; no tag was created or pushed.\n'
  exit 0
fi

git tag -a "$next" -m "Release $next"
if ! GIT_TERMINAL_PROMPT=0 git push origin "refs/tags/$next"; then
  fail "push failed; $next remains local. Inspect the remote before retrying: git push origin refs/tags/$next"
fi
printf 'Pushed %s. GitHub Actions will build and publish the release.\n' "$next"
