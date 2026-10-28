# omaquery

`omaquery` asks for values in an Omarchy desktop popup, then passes them to a command as environment variables. It supports local prompts and prompts on another Omarchy machine over ordinary SSH.

Prompting requires Omarchy (`ID=omarchy` in `/etc/os-release`) on both machines. Help and version still work elsewhere.

## Install

On an Omarchy desktop with Quickshell:

```sh
curl -fsSL https://raw.githubusercontent.com/peterszarvas94/omaquery/master/install.sh | bash
```

The installer builds the CLI in `~/.local/bin` and enables the popup plugin. It installs Go through Omarchy if needed. Keep `~/.local/bin` on your `PATH`. Run the installer on each machine that will show prompts.

To install just the CLI with Go (the popup still needs the installer above):

```sh
go install github.com/peterszarvas94/omaquery/cmd/omaquery@latest
```

Manual install (requires Go 1.24+):

```sh
git clone https://github.com/peterszarvas94/omaquery.git
cd omaquery
make install
omarchy-shell shell rescanPlugins
omarchy plugin enable omaquery.prompt
```

After a manual plugin update, run `omarchy restart shell`.

## Commands

| Command | Purpose |
| --- | --- |
| `omaquery run [options] NAME[:hint] ... -- COMMAND [ARG ...]` | Prompt for values and run a command with them in its environment. |
| `omaquery version` or `omaquery --version` | Print the version. |
| `omaquery help`, `omaquery --help`, or `omaquery run --help` | Show Cobra help. |

`run` options: `--reason TEXT`, `--timeout 5m` (or seconds), and `--prompt-on SSH_HOST`. Options come before value names. `serve` and `relay` are internal commands used by the popup and SSH transport.

```sh
omaquery run API_TOKEN -- ./deploy
omaquery run --prompt-on laptop API_TOKEN -- ./deploy
```

The command runs where you invoke `omaquery`, even when the popup appears on `laptop`. Remote prompting needs SSH access and an unlocked Omarchy desktop on the target. The SSH connection lasts only for the request; `omaquery` opens no network listener or secret store.

To try it without a real secret:

```sh
omaquery run DEMO_TOKEN -- sh -c 'printf "%s\n" "$DEMO_TOKEN"'
```

Enter dummy text in the popup; it prints to your terminal. Review the displayed command before submitting: a command can print or transmit its environment. Escape cancels the prompt. See [AGENTS.md](AGENTS.md) for development notes and [LICENSE](LICENSE) for the license.
