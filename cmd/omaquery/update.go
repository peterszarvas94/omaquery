package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"time"
)

const modulePath = "github.com/peterszarvas94/omaquery"

var latestVersionURL = "https://proxy.golang.org/" + modulePath + "/@latest"

var runLatestInstall = func(ctx context.Context, latest, remote string, force bool, out, errOut io.Writer) error {
	args := []string{"run", modulePath + "/cmd/omaquery@" + latest, "install"}
	if remote != "" {
		args = append(args, "--remote", remote)
	}
	if force {
		args = append(args, "--force")
	}
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Stdout = out
	cmd.Stderr = errOut
	return cmd.Run()
}

func latestVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestVersionURL, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("latest version: HTTP %s", resp.Status)
	}
	var release struct {
		Version string `json:"Version"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&release); err != nil {
		return "", err
	}
	if release.Version == "" {
		return "", errors.New("latest version is empty")
	}
	return release.Version, nil
}

func update(ctx context.Context, remote string, force bool, out, errOut io.Writer) error {
	latest, err := latestVersion(ctx)
	if err != nil {
		return fmt.Errorf("check latest release: %w", err)
	}
	if compareVersions(latest, version) <= 0 {
		fmt.Fprintf(out, "Already on latest omaquery %s\n", version)
		if remote == "" {
			return nil
		}
		// The local binary is current, but the remote machine may not be.
		self, err := os.Executable()
		if err != nil {
			return err
		}
		return installRemote(ctx, remote, self, force, out)
	}

	fmt.Fprintf(out, "Updating omaquery %s -> %s\n", version, latest)
	return runLatestInstall(ctx, latest, remote, force, out, errOut)
}
