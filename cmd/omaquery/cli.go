package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

const version = "0.2.0"

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
			"another Omarchy machine over SSH.",
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

func newRunCommand(execute func(request, time.Duration) int, code *int) *cobra.Command {
	var reason, timeout, promptOn string
	cmd := &cobra.Command{
		Use:   "run [flags] NAME[:hint]... -- COMMAND [ARG...]",
		Short: "Prompt for values and run a command",
		Long: "Prompt for up to 16 values and pass them to the command's environment.\n" +
			"The command runs on this machine, even when --prompt-on shows the popup\n" +
			"elsewhere. Review the displayed command before submitting: it can print\n" +
			"or transmit its environment. Escape cancels the prompt.",
		Example: "  omaquery run API_TOKEN -- ./deploy\n  omaquery run --prompt-on laptop API_TOKEN -- ./deploy\n  omaquery run API_TOKEN -- sh -c 'printf \"%s\\n\" \"$API_TOKEN\"'",
		RunE: func(cmd *cobra.Command, args []string) error {
			req, duration, err := parseRun(args, cmd.Flags().ArgsLenAtDash(), reason, timeout, promptOn)
			if err != nil {
				return err
			}
			*code = execute(req, duration)
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "Reason shown in the popup")
	cmd.Flags().StringVar(&timeout, "timeout", "5m", "Prompt timeout (seconds or Go duration; max 24h)")
	cmd.Flags().StringVar(&promptOn, "prompt-on", "", "SSH host on which to show the popup")
	return cmd
}

func parseTimeout(value string) (time.Duration, error) {
	if seconds, err := strconv.ParseFloat(value, 64); err == nil {
		return time.Duration(seconds * float64(time.Second)), nil
	}
	return time.ParseDuration(value)
}
