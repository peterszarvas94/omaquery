# omaquery

`omaquery` opens a small Omarchy desktop prompt when a command needs a value, such as an API token. You see the command and the names of the requested values before you approve it. The values go into that command's environment; `omaquery run` does not print them.

```sh
omaquery run API_TOKEN -- ./deploy
```

The command runs on your current machine. To show the prompt on another Omarchy desktop reachable by SSH, add `--prompt-on`:

```sh
omaquery run --prompt-on laptop API_TOKEN -- ./deploy
```

The SSH connection lasts only for that request. The command still runs on the machine where you invoked `omaquery`.

## Install

You need a current Omarchy desktop with its Quickshell plugin system, Go 1.24 or newer, and `~/.local/bin` on the desktop shell's `PATH`.

Clone this repository. From its root, run:

```sh
make install
omarchy-shell shell rescanPlugins
omarchy plugin enable omaquery.prompt
```

`make install` builds the CLI in `~/.local/bin` and places the popup plugin in `~/.config/omarchy/plugins/omaquery.prompt`. The plugin starts a local bridge inside the existing Omarchy shell. If the enable command says the plugin is unknown immediately after a rescan, repeat it once discovery finishes.

After updating the plugin, run `make install` and `omarchy restart shell`. Omarchy retains loaded service plugins, so a rescan alone will not replace their QML code.

For a prompt on another machine, install and enable `omaquery` on that machine too. It must have a logged-in, unlocked Omarchy desktop and ordinary SSH access from the requesting machine. Verify SSH before trying `omaquery`:

```sh
ssh -o BatchMode=yes -o StrictHostKeyChecking=yes laptop true
```

The remote SSH account must have `~/.local/bin/omaquery`. `--prompt-on` accepts an SSH host, `user@host`, IP address, or SSH config alias. It works over Tailscale, Headscale, or another network that carries SSH. It does not require Tailscale SSH or Tailscale Serve. SSH host-key verification and batch mode are mandatory; an unknown host key or unavailable desktop stops the request.

## Use

```sh
omaquery run --reason 'Run integration tests' TEST_TOKEN -- npm test
omaquery run --timeout 90 'API_TOKEN:Token with deploy access' -- ./deploy
omaquery run --prompt-on laptop API_TOKEN -- ./deploy
```

Options go before the value names. A hint after a colon tells the person where to find a value. The timeout accepts seconds (`90`) or Go durations (`90s`, `5m`), defaults to five minutes, and covers waiting for and filling in the prompt. It does not limit the command's run time. You can request up to 16 nonempty values at once.

The popup shows the working directory, exact command arguments, reason, and hints. Arguments are quoted so spaces and shell syntax are visible. The command runs directly, without an implicit shell. Enter moves to the next field or submits from the last one; Escape cancels. Multiple requests queue. A request disappears if its caller disconnects or its timeout expires.

Try it with a dummy value:

```sh
omaquery run DEMO_TOKEN -- sh -c 'test -n "$DEMO_TOKEN" && echo "Value received"'
```

Enter any dummy text in the popup. The command prints only `Value received`.

`omaquery run` returns the command's exit status. It returns 1 if you cancel, 124 if the prompt times out, 2 for a setup or protocol error, and 127 if the command cannot start.

## How values are handled

The local popup and CLI communicate through a Unix socket under `$XDG_RUNTIME_DIR/omaquery`. The directory is owned by your user and has mode 0700; the socket has mode 0600. Values travel from the popup to the CLI through local pipes and that socket. A remote prompt uses one ordinary SSH process and sends a small heartbeat while it waits, so a dropped connection closes the remote popup promptly. `omaquery` opens no network listener of its own and stores no secret cache or dotenv file.

The remote popup shows the hostname reported by the requester and the peer IP seen by SSH. The hostname is a label supplied by the requester; use the SSH peer and the displayed command when deciding whether to approve. Whoever controls the approved command can print or transmit its environment. Code running as your user can also inspect or spoof local requests. `omaquery` helps keep values out of the normal agent transcript; it is not a sandbox against code you choose to run.

The internal `relay` command returns values through an SSH stdout pipe consumed by the requesting `omaquery` process. Do not invoke it directly to collect secrets. This version does not include a 1Password picker or a command that prints values for agents.

## Develop

```sh
make build
make test
make check
```

`make check` runs Go tests with the race detector, `go vet`, Omarchy plugin validation, and `qmllint`. The tests use dummy values and cover local and remote transport, cancellation, timeout, disconnected callers, stale replies, and command exit behavior. For a desktop check, install a build and run the dummy command above. Remote checks also need SSH and an unlocked desktop on the target.

See [AGENTS.md](AGENTS.md) for repository conventions and [LICENSE](LICENSE) for the MIT license.
