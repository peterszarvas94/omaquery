# omaquery

`omaquery` asks for values in an Omarchy desktop popup and passes them to a
command as environment variables. Prompts can appear on the local machine or on
another Omarchy machine over SSH.

![omaquery prompt](preview.png)

Prompting requires Omarchy (`ID=omarchy` in `/etc/os-release`) and Quickshell.
Help and version still work elsewhere.

## Requirements

- Omarchy with Quickshell
- Go 1.24+ to install or build the CLI
- SSH key access and an unlocked Omarchy desktop for remote prompts

## Install

Install the CLI and its prompt plugin:

```sh
omarchy pkg add go
go install github.com/peterszarvas94/omaquery/cmd/omaquery@latest
omaquery install
```

Or do the fresh install in one command:

```sh
go run github.com/peterszarvas94/omaquery/cmd/omaquery@latest install
```

`omaquery install` installs the version you are running and writes the matching
plugin. It validates and enables the plugin, restarts the Omarchy shell when
updating an already-enabled plugin, and is safe to rerun. It does not fetch a
newer release. Keep `~/.local/bin` on `PATH`.

The marketplace plugin install only installs the Quickshell service. The
service still needs the matching CLI; run the commands above after installing
from the marketplace. `omaquery install` creates and enables the service when
needed. If the plugin came from the plugin manager, the CLI takes over its
updates so there is only one installed version.

## Update

Check for and install the latest release, together with its matching plugin:

```sh
omaquery update
```

`omaquery update` fetches and runs the latest release. For another Omarchy
machine:

```sh
omaquery update --remote laptop
```

Local and remote installs, updates, and removals finish with an Omarchy
notification when the desktop session is reachable.

Binaries older than `v0.7.0` cannot self-update. Bootstrap them once with
`go install github.com/peterszarvas94/omaquery/cmd/omaquery@latest` followed by
`omaquery install`, or with the single `go run` command above.

### From a checkout

```sh
git clone https://github.com/peterszarvas94/omaquery.git
cd omaquery
make install
```

## Remove

On each machine with the prompt installed:

```sh
omaquery uninstall
```

The command removes the plugin first, then deletes the installed binary, so a
partial failure leaves the CLI available to retry. For another machine:

```sh
omaquery uninstall --remote laptop
```

Removing the `omaquery` binary disables local and remote prompts. No secrets or
other persistent files are kept by the application.

## Usage

```sh
omaquery run API_TOKEN -- ./deploy
omaquery run --reason "Publish the release" API_TOKEN -- gh release create ...
omaquery run --remote laptop API_TOKEN -- ./deploy
```

`run` options are `--reason TEXT`, `--timeout 5m` (or seconds), and
`--remote SSH_HOST`. Options come before value names. `--prompt-on` remains as a
deprecated alias for `--remote`.

Request multiple secrets by listing their environment variable names before
`--`. They appear together in one popup, and each value is passed to the command
under its corresponding name (up to 16 fields):

```sh
omaquery run API_TOKEN DATABASE_PASSWORD -- ./deploy
```

Add a hint after a colon using `NAME:hint`. Quote the whole argument when the
hint contains spaces. Hints appear below the field names in the popup; they do
not change the environment variable names. Use them to describe what to enter,
not to supply secret values:

```sh
omaquery run --reason "Deploy the application" \
  'API_TOKEN:Deployment API token' \
  'DATABASE_PASSWORD:Password for the application database' \
  -- ./deploy
```

To collect both secrets on another Omarchy machine, add `--remote laptop` before
the field arguments. The popup appears on `laptop`, while `./deploy` runs on the
machine where you invoked the command:

```sh
omaquery run --remote laptop --reason "Deploy the application" \
  'API_TOKEN:Deployment API token' \
  'DATABASE_PASSWORD:Password for the application database' \
  -- ./deploy
```

The command runs where you invoke `omaquery`, even when the popup appears on
another machine. Review the displayed command before submitting: a command can
print or transmit its environment. Escape cancels the prompt.

To try it without a real secret:

```sh
omaquery run DEMO_TOKEN -- sh -c 'printf "%s\n" "$DEMO_TOKEN"'
```

## Security

- Values are sent only to the approved command's environment. They are never
  written to logs, command arguments, ordinary stdout, or persistent files.
- The local prompt bridge uses an owned mode-0700 runtime directory and a
  mode-0600 Unix socket.
- Remote prompts use ordinary SSH with host-key checking and batch mode. The
  plugin opens no network listener and runs no privileged commands.
- Like every Omarchy plugin, the popup service runs unsandboxed inside the
  Omarchy shell. The service only starts `omaquery serve` and exchanges prompt
  data with the CLI.
- A command can print or transmit its own environment. This tool keeps values
  out of the agent's direct input; it cannot constrain what the approved command
  does with them.

## License

MIT. See [LICENSE](LICENSE). Development notes are in [AGENTS.md](AGENTS.md).
