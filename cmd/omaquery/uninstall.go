package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
)

func uninstallLocal(ctx context.Context, out io.Writer) error {
	bundle, err := loadPluginBundle()
	if err != nil {
		return err
	}
	ids := []string{bundle.ID}
	for _, id := range legacyPluginIDs {
		if id != bundle.ID {
			ids = append(ids, id)
		}
	}

	env, envErr := installEnvironment(ctx)
	removed := false
	fallback := false
	for _, id := range ids {
		if envErr != nil {
			pluginRemoved, err := removePluginFiles(id)
			if err != nil {
				return err
			}
			removed = removed || pluginRemoved
			continue
		}
		pluginRemoved, usedFallback, err := removePlugin(ctx, env, id)
		if err != nil {
			return err
		}
		removed = removed || pluginRemoved
		fallback = fallback || usedFallback
	}
	if fallback {
		if _, err := runInstallCommand(ctx, env, "omarchy", "restart", "shell"); err != nil {
			return fmt.Errorf("plugin removed, but shell restart failed: %w", err)
		}
	}

	binaryPath, err := binaryInstallPath()
	if err != nil {
		return err
	}
	if err := os.Remove(binaryPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", binaryPath, err)
	}
	if removed {
		fmt.Fprintln(out, "Removed omaquery")
	} else {
		fmt.Fprintln(out, "omaquery was not installed")
	}
	fmt.Fprintf(out, "Binary: %s\n", binaryPath)
	return nil
}

func uninstallRemote(ctx context.Context, target, executable string, out io.Writer) error {
	if !validTarget(target) {
		return fmt.Errorf("remote must be an SSH host or user@host without options")
	}
	if runtime.GOOS != "linux" {
		return fmt.Errorf("remote uninstall requires a Linux build of omaquery")
	}
	state, err := probeRemote(ctx, target)
	if err != nil {
		return err
	}
	if state.Arch != "" && !archMatches(state.Arch) {
		return fmt.Errorf("remote machine is %s but this omaquery binary is %s; install a matching binary on the remote first", state.Arch, runtime.GOARCH)
	}
	fmt.Fprintf(out, "Removing omaquery from %s\n", target)
	return runRemoteBinary(ctx, target, executable, "uninstall", out)
}
