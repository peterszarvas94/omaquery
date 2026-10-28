# Repository guidance

`omaquery` is a Go CLI and an Omarchy Quickshell service plugin. The CLI is in `cmd/omaquery/`; the popup is `Popup.qml` with `manifest.json`. Keep the plugin in Omarchy's existing shell process.

## Before changing code

- Use a task branch and pull request. Do not edit Omarchy's packaged files under `/usr/share/omarchy/`.
- Read the affected Go and QML paths together. The CLI, local bridge, remote relay, and popup share one request/response protocol.
- Keep examples free of personal names, machine names, private domains, addresses, tokens, and workstation paths. Use `laptop`, `desktop.example.test`, and dummy values. Public GitHub install URLs may name this repository's owner.
- Do not use Python scripts in this repository's workflow.

## Security invariants

- `omaquery run` passes collected values only to the approved command's environment. Never add them to logs, error text, command arguments, ordinary stdout, or persistent files.
- Show the exact command and working directory before approval. Distinguish the requester-supplied hostname from the peer address seen by SSH.
- Keep the local socket in an owned mode-0700 runtime directory with a mode-0600 socket. Reject invalid fields, oversized messages, stale replies, and unsafe SSH targets.
- Remote requests use ordinary SSH with host-key checking and batch mode. They open no `omaquery` network listener. A caller disconnect, cancellation, or timeout must close the remote popup.
- The internal `relay` protocol carries values in a private SSH stdout pipe. Never expose that pipe as agent-visible output. Do not claim this protects against an arbitrary command or other code running as the same user.

## Validation

Run `make check` for Go tests, race detection, `go vet`, manifest validation, and QML linting. Exercise real desktop prompts with dummy values when the UI or transport changes. For remote changes, verify a successful round trip and timeout cleanup on two desktops when they are available. State clearly when a live check could not run.

Use `make install` to deploy a fresh build for desktop testing. After changing a retained service plugin, run `omarchy restart shell` and check `omarchy-shell shell ping`. Never silently reset user configuration.
