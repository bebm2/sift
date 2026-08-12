#!/usr/bin/env bash
# Run a command inside a fresh, auto-cleaned sandbox.
#
# Why this exists: testing sift against an isolated SIFT_HOME used to mean
# emitting `T=$(mktemp -d); ...; rm -rf "$T"` in every probe, which triggers a
# per-command confirmation. This script owns the mktemp + the trap cleanup, so
# callers never write a bare `rm -rf` and stay confirmation-free.
#
# Usage:
#   scripts/dev/with-sandbox.sh [--build] [--keep] -- <command...>
#
# Options (before `--`):
#   --build   Build sift + sift-agent-wrapper into $SANDBOX/bin/<ver>/ and set
#             up the bin/current symlink + prepend it to PATH, so commands like
#             `sift service install` / `sift daemon` resolve a real release.
#   --keep    Do NOT clean up on exit; print the sandbox path for inspection.
#
# Environment exported to <command>:
#   SANDBOX   the temp dir (absolute, mode 0700).
#   SIFT_HOME = $SANDBOX  (so sift never touches the real ~/.sift).
#
# The sandbox is removed on exit (trap) unless --keep. Only the mktemp dir is
# ever removed — never $SIFT_HOME if a caller overrode it, never anything else.
# For launchd/systemd lifecycle tests, have <command> run `sift service
# uninstall` itself; this script only owns the on-disk home.
set -euo pipefail

build=0
keep=0
gc=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    --build) build=1; shift ;;
    --keep)  keep=1;  shift ;;
    --gc)    gc=1;    shift ;;
    --)      shift; break ;;
    -h|--help)
      sed -n '2,/^set -euo/p' "$0" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *) break ;;
  esac
done

# --gc: sweep leftover sift-sandbox.* dirs (e.g. from --keep) without the
# caller ever writing a bare rm -rf. Intended as `scripts/dev/with-sandbox.sh --gc`.
if [ "$gc" -eq 1 ]; then
  count=0
  for d in "${TMPDIR:-/tmp}"/sift-sandbox.*; do
    [ -e "$d" ] || continue
    chmod -R u+w "$d" 2>/dev/null || true
    rm -rf -- "$d"
    count=$((count + 1))
  done
  printf 'swept %d sandbox dir(s)\n' "$count"
  exit 0
fi

if [ "$#" -eq 0 ]; then
  echo "usage: $0 [--build] [--keep] -- <command...>" >&2
  exit 2
fi

SANDBOX="$(mktemp -d "${TMPDIR:-/tmp}/sift-sandbox.XXXXXX")"
chmod 700 "$SANDBOX"
export SANDBOX SIFT_HOME="$SANDBOX"

cleanup() {
  if [ "$keep" -eq 1 ]; then
    printf 'sandbox kept at %s\n' "$SANDBOX" >&2
    return
  fi
  chmod -R u+w "$SANDBOX" 2>/dev/null || true
  rm -rf -- "$SANDBOX"
}
trap cleanup EXIT

if [ "$build" -eq 1 ]; then
  # Resolve repo root from this script's location so `go build` works from any cwd.
  root="$(cd "$(dirname "$0")/../.." && pwd)"
  ver="0.0.0-sandbox"
  mkdir -p "$SANDBOX/bin/$ver"
  ( cd "$root" && CGO_ENABLED=0 go build -o "$SANDBOX/bin/$ver/sift" ./cmd/sift )
  ( cd "$root" && CGO_ENABLED=0 go build -o "$SANDBOX/bin/$ver/sift-agent-wrapper" ./cmd/sift-agent-wrapper )
  ln -sfn "$ver" "$SANDBOX/bin/current"
  export PATH="$SANDBOX/bin/current:$PATH"
fi

"$@"
