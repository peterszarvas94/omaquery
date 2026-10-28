#!/usr/bin/env bash
set -euo pipefail

repo='peterszarvas94/omaquery'
archive="https://github.com/${repo}/archive/refs/heads/master.tar.gz"

die() { printf 'omaquery install: %s\n' "$*" >&2; exit 1; }

[[ -r /etc/os-release ]] || die 'requires an Omarchy machine'
. /etc/os-release
[[ ${ID:-} == omarchy ]] || die 'requires an Omarchy machine'

for command in curl tar omarchy omarchy-shell; do
  command -v "$command" >/dev/null || die "missing $command"
done
omarchy-shell shell ping >/dev/null || die 'start and unlock the Omarchy desktop first'

if ! command -v go >/dev/null; then
  [[ -r /dev/tty ]] || die 'Go is missing; install it with: omarchy pkg add go'
  omarchy pkg add go </dev/tty
fi

work_dir=$(mktemp -d)
trap 'rm -r -- "$work_dir"' EXIT
curl -fsSL --retry 3 "$archive" | tar -xz -C "$work_dir"
source_dir="${work_dir}/omaquery-master"
[[ -f "${source_dir}/go.mod" ]] || die 'source archive is incomplete'

printf 'Building omaquery...\n'
( cd "$source_dir" && go build -trimpath -o "${work_dir}/omaquery" ./cmd/omaquery )

"${work_dir}/omaquery" install

if [[ :$PATH: != *":${HOME}/.local/bin:"* ]]; then
  printf 'Add %s/.local/bin to your PATH to use omaquery.\n' "$HOME"
fi
