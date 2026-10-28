#!/usr/bin/env bash
set -euo pipefail

repo='peterszarvas94/omaquery'
archive="https://github.com/${repo}/archive/refs/heads/master.tar.gz"
plugin_id='omaquery.prompt'
plugin_dir="${HOME}/.config/omarchy/plugins/${plugin_id}"
binary="${HOME}/.local/bin/omaquery"

die() { printf 'omaquery install: %s\n' "$*" >&2; exit 1; }

[[ -r /etc/os-release ]] || die 'requires an Omarchy machine'
. /etc/os-release
[[ ${ID:-} == omarchy ]] || die 'requires an Omarchy machine'

for command in curl tar install omarchy omarchy-shell; do
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
had_plugin=false
[[ -d "$plugin_dir" ]] && had_plugin=true

install -Dm755 "${work_dir}/omaquery" "$binary"
install -Dm644 "${source_dir}/manifest.json" "${plugin_dir}/manifest.json"
install -Dm644 "${source_dir}/Popup.qml" "${plugin_dir}/Popup.qml"
omarchy plugin validate "$plugin_dir"
omarchy-shell shell rescanPlugins >/dev/null

enabled=false
for attempt in {1..10}; do
  if omarchy plugin enable "$plugin_id" >/dev/null 2>&1; then
    enabled=true
    break
  fi
  sleep 0.5
done
[[ $enabled == true ]] || die "could not enable ${plugin_id}; run: omarchy plugin enable ${plugin_id}"

if [[ $had_plugin == true ]]; then
  omarchy restart shell
fi

printf 'Installed %s and enabled %s.\n' "$binary" "$plugin_id"
if [[ :$PATH: != *":${HOME}/.local/bin:"* ]]; then
  printf 'Add %s/.local/bin to your PATH to use omaquery.\n' "$HOME"
fi
