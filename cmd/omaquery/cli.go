package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

const version = "0.7.0"

func main() { os.Exit(mainCode(os.Args[1:])) }

func mainCode(args []string) int {
	code := 0
	root := newRootCommand(&code)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		return fail(err)
	}
	return code
}

func newRootCommand(code *int) *cobra.Command {
	root := &cobra.Command{
		Use:   "omaquery",
		Short: "Request values through an Omarchy desktop popup",
		Long: "Request values through an Omarchy desktop popup and pass them to a\n" +
			"command as environment variables. Prompts can appear locally or on\n" +
			"another Omarchy machine over SSH. The experimental --mobile path uses\n" +
			"an explicitly enabled Android companion bridge.",
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.SetVersionTemplate("omaquery {{.Version}}\n")
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(newRunCommand(func(req request, duration time.Duration) int {
		return runRequest(req, duration)
	}, code))
	root.AddCommand(newMobileCommand())
	root.AddCommand(newInstallCommand())
	root.AddCommand(newUpdateCommand())
	root.AddCommand(newUninstallCommand())
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "omaquery %s\n", version)
		},
	})
	root.AddCommand(&cobra.Command{
		Use:    "serve",
		Hidden: true,
		Args:   cobra.NoArgs,
		Run: func(_ *cobra.Command, _ []string) {
			if err := requireOmarchy(); err != nil {
				*code = fail(err)
				return
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			if err := serve(ctx, os.Stdin, os.Stdout); err != nil {
				*code = fail(err)
			}
		},
	})
	root.AddCommand(&cobra.Command{
		Use:    "relay",
		Hidden: true,
		Args:   cobra.NoArgs,
		Run: func(_ *cobra.Command, _ []string) {
			*code = relay(os.Stdin, os.Stdout)
		},
	})
	return root
}

func newInstallCommand() *cobra.Command {
	var remote string
	var force bool
	cmd := &cobra.Command{
		Use:   "install [flags]",
		Short: "Install the current omaquery version and Omarchy prompt",
		Long: "Install or repair the CLI and prompt plugin on this Omarchy machine.\n" +
			"With --remote, upload this omaquery version to another Omarchy machine over\n" +
			"SSH and install it there. Use update to fetch a newer release.",
		Example: "  omaquery install\n  omaquery install --remote laptop",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if remote != "" && !validTarget(remote) {
				return errors.New("remote must be an SSH host or user@host without options")
			}
			self, err := os.Executable()
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			if remote == "" {
				return installLocal(ctx, self, cmd.OutOrStdout())
			}
			return installRemote(ctx, remote, self, force, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&remote, "remote", "", "SSH machine on which to install or repair")
	cmd.Flags().BoolVar(&force, "force", false, "Install even when the remote version is newer")
	return cmd
}

func newUpdateCommand() *cobra.Command {
	var remote string
	var force bool
	cmd := &cobra.Command{
		Use:   "update [flags]",
		Short: "Update omaquery and its Omarchy prompt to the latest release",
		Long: "Check for the latest omaquery release and install it together with the\n" +
			"matching prompt plugin. Use --remote to update another Omarchy machine.\n" +
			"Use install instead to reinstall the version already on this machine.",
		Example: "  omaquery update\n  omaquery update --remote laptop",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if remote != "" && !validTarget(remote) {
				return errors.New("remote must be an SSH host or user@host without options")
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return update(ctx, remote, force, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&remote, "remote", "", "SSH machine on which to update")
	cmd.Flags().BoolVar(&force, "force", false, "Update even when the remote version is newer")
	return cmd
}

func newUninstallCommand() *cobra.Command {
	var remote string
	cmd := &cobra.Command{
		Use:   "uninstall [flags]",
		Short: "Remove omaquery and its Omarchy prompt",
		Long: "Remove the prompt plugin, then delete the installed omaquery binary.\n" +
			"The plugin is removed first so a failed uninstall can be retried.\n" +
			"With --remote, run the same removal on another Omarchy machine over SSH.",
		Example: "  omaquery uninstall\n  omaquery uninstall --remote laptop",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if remote != "" && !validTarget(remote) {
				return errors.New("remote must be an SSH host or user@host without options")
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			if remote == "" {
				return uninstallLocal(ctx, cmd.OutOrStdout())
			}
			self, err := os.Executable()
			if err != nil {
				return err
			}
			return uninstallRemote(ctx, remote, self, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&remote, "remote", "", "SSH machine from which to uninstall")
	return cmd
}

func newRunCommand(execute func(request, time.Duration) int, code *int) *cobra.Command {
	var reason, timeout, remote, legacyRemote string
	var mobile bool
	cmd := &cobra.Command{
		Use:   "run [flags] NAME[:hint]... -- COMMAND [ARG...]",
		Short: "Prompt for values and run a command",
		Long: "Prompt for up to 16 values and pass them to the command's environment.\n" +
			"The command runs on this machine, even when --remote shows the popup\n" +
			"elsewhere or --mobile requests values on Android. Review the displayed\n" +
			"command before submitting: it can print or transmit its environment.\n" +
			"Escape cancels desktop prompts; use Reject request on Android.",
		Example: "  omaquery run API_TOKEN -- ./deploy\n  omaquery run --remote laptop API_TOKEN -- ./deploy\n  omaquery run API_TOKEN -- sh -c 'printf \"%s\\n\" \"$API_TOKEN\"'",
		RunE: func(cmd *cobra.Command, args []string) error {
			if legacyRemote != "" {
				if remote != "" && remote != legacyRemote {
					return errors.New("use only --remote")
				}
				remote = legacyRemote
			}
			req, duration, err := parseRun(args, cmd.Flags().ArgsLenAtDash(), reason, timeout, remote)
			if err != nil {
				return err
			}
			if mobile && remote != "" {
				return errors.New("--mobile and --remote cannot be combined")
			}
			req.Mobile = mobile
			*code = execute(req, duration)
			return nil
		},
	}
	cmd.Flags().BoolVar(&mobile, "mobile", false, "Request values from the experimental Android companion")
	cmd.Flags().StringVar(&reason, "reason", "", "Reason shown in the popup")
	cmd.Flags().StringVar(&timeout, "timeout", "5m", "Prompt timeout (seconds or Go duration; max 24h)")
	cmd.Flags().StringVar(&remote, "remote", "", "SSH machine on which to show the popup")
	cmd.Flags().StringVar(&legacyRemote, "prompt-on", "", "Deprecated: use --remote")
	_ = cmd.Flags().MarkDeprecated("prompt-on", "use --remote instead")
	return cmd
}

func parseTimeout(value string) (time.Duration, error) {
	if seconds, err := strconv.ParseFloat(value, 64); err == nil {
		return time.Duration(seconds * float64(time.Second)), nil
	}
	return time.ParseDuration(value)
}
