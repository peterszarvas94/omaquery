package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/peterszarvas94/omaquery"
)

var pluginIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

var runInstallCommand = func(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if env != nil {
		cmd.Env = env
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, detail)
		}
		return out, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return out, nil
}

// installEnvironment supplies the variables an SSH install does not inherit
// from the desktop session, especially OMARCHY_PATH. Without it omarchy-shell
// refuses to rescan or enable the plugin.
func installEnvironment(ctx context.Context) ([]string, error) {
	env := append([]string(nil), os.Environ()...)
	value := func(key string) string {
		prefix := key + "="
		for i := len(env) - 1; i >= 0; i-- {
			if strings.HasPrefix(env[i], prefix) {
				return strings.TrimPrefix(env[i], prefix)
			}
		}
		return ""
	}
	set := func(key, value string) {
		prefix := key + "="
		for i := len(env) - 1; i >= 0; i-- {
			if strings.HasPrefix(env[i], prefix) {
				env[i] = prefix + value
				return
			}
		}
		env = append(env, prefix+value)
	}

	if value("XDG_RUNTIME_DIR") == "" {
		set("XDG_RUNTIME_DIR", fmt.Sprintf("/run/user/%d", os.Getuid()))
	}
	if path := value("OMARCHY_PATH"); path != "" && omarchyPathValid(path) {
		return env, nil
	}

	// A session opened by SSH has no desktop environment. Ask the user manager
	// for the environment the graphical session started with.
	cmd := exec.CommandContext(ctx, "systemctl", "--user", "show-environment")
	cmd.Env = env
	if out, err := cmd.Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			key, path, found := strings.Cut(line, "=")
			if found && key == "OMARCHY_PATH" && omarchyPathValid(path) {
				set("OMARCHY_PATH", path)
				return env, nil
			}
		}
	}

	home, _ := os.UserHomeDir()
	for _, path := range []string{"/usr/share/omarchy", filepath.Join(home, ".local", "share", "omarchy")} {
		if omarchyPathValid(path) {
			set("OMARCHY_PATH", path)
			return env, nil
		}
	}
	return nil, errors.New("OMARCHY_PATH is not set and no Omarchy installation was found")
}

func omarchyPathValid(path string) bool {
	_, err := os.Stat(filepath.Join(path, "shell", "shell.qml"))
	return err == nil
}

type pluginBundle struct {
	ID       string
	Version  string
	Manifest []byte
	Popup    []byte
}

func loadPluginBundle() (pluginBundle, error) {
	var bundle pluginBundle
	var err error
	bundle.Manifest, err = omaquery.PluginFS.ReadFile("manifest.json")
	if err != nil {
		return bundle, err
	}
	bundle.Popup, err = omaquery.PluginFS.ReadFile("Popup.qml")
	if err != nil {
		return bundle, err
	}

	var manifest struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(bundle.Manifest, &manifest); err != nil {
		return bundle, fmt.Errorf("embedded plugin manifest: %w", err)
	}
	if !pluginIDPattern.MatchString(manifest.ID) || strings.Contains(manifest.ID, "..") || strings.HasPrefix(manifest.ID, "omarchy.") {
		return bundle, fmt.Errorf("embedded plugin id is invalid: %q", manifest.ID)
	}
	bundle.ID = manifest.ID
	bundle.Version = manifest.Version
	return bundle, nil
}

func configHome() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(dir) {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
}

func pluginInstallDir(id string) (string, error) {
	config, err := configHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(config, "omarchy", "plugins", id), nil
}

func binaryInstallPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "bin", "omaquery"), nil
}

func installedPluginVersion(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return ""
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &manifest) != nil {
		return ""
	}
	return manifest.Version
}

func pluginEnabled(ctx context.Context, env []string, id string) bool {
	out, err := runInstallCommand(ctx, env, "omarchy", "plugin", "list", "--json")
	if err != nil {
		return false
	}
	var plugins []struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
	if json.Unmarshal(out, &plugins) != nil {
		return false
	}
	for _, plugin := range plugins {
		if plugin.ID == id {
			return plugin.Enabled
		}
	}
	return false
}

func enablePlugin(ctx context.Context, env []string, id string) error {
	if pluginEnabled(ctx, env, id) {
		return nil
	}
	if _, err := runInstallCommand(ctx, env, "omarchy", "plugin", "enable", id); err != nil {
		// The list probe can race plugin discovery immediately after a rescan.
		if pluginEnabled(ctx, env, id) {
			return nil
		}
		return err
	}
	return nil
}

func installLocal(ctx context.Context, executable string, out io.Writer) error {
	if err := requireOmarchy(); err != nil {
		return err
	}
	if executable == "" {
		return errors.New("cannot locate the running omaquery executable")
	}
	bundle, err := loadPluginBundle()
	if err != nil {
		return err
	}
	env, err := installEnvironment(ctx)
	if err != nil {
		return err
	}
	pluginDir, err := pluginInstallDir(bundle.ID)
	if err != nil {
		return err
	}
	binaryPath, err := binaryInstallPath()
	if err != nil {
		return err
	}
	if info, err := os.Lstat(pluginDir); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("plugin directory is a symlink: %s", pluginDir)
	}

	previousVersion := installedPluginVersion(pluginDir)
	wasEnabled := pluginEnabled(ctx, env, bundle.ID)
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(pluginDir, "manifest.json"), bundle.Manifest, 0644); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(pluginDir, "Popup.qml"), bundle.Popup, 0644); err != nil {
		return err
	}
	if _, err := runInstallCommand(ctx, env, "omarchy", "plugin", "validate", pluginDir); err != nil {
		return err
	}
	if err := installExecutable(executable, binaryPath); err != nil {
		return err
	}
	if _, err := runInstallCommand(ctx, env, "omarchy-shell", "shell", "rescanPlugins"); err != nil {
		return err
	}
	if err := enablePlugin(ctx, env, bundle.ID); err != nil {
		return err
	}
	if wasEnabled {
		if _, err := runInstallCommand(ctx, env, "omarchy", "restart", "shell"); err != nil {
			return fmt.Errorf("installed, but shell restart failed: %w", err)
		}
	}

	action := "Installed"
	if previousVersion != "" {
		action = "Updated"
	}
	if previousVersion != "" && previousVersion != bundle.Version {
		fmt.Fprintf(out, "%s omaquery %s -> %s\n", action, previousVersion, bundle.Version)
	} else {
		fmt.Fprintf(out, "%s omaquery %s\n", action, bundle.Version)
	}
	fmt.Fprintf(out, "Binary: %s\nPlugin: %s\n", binaryPath, pluginDir)
	return nil
}

func installExecutable(source, target string) error {
	sourceInfo, err := os.Stat(source)
	if err != nil {
		return err
	}
	if targetInfo, err := os.Stat(target); err == nil && os.SameFile(sourceInfo, targetInfo) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(target), ".omaquery-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, target)
}

func writeFileAtomic(path string, data []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".omaquery-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

type remoteInstallState struct {
	Arch    string
	Version string
}

func probeRemote(ctx context.Context, target string) (remoteInstallState, error) {
	const script = `. /etc/os-release 2>/dev/null || { echo "omaquery install: requires Omarchy on the remote machine" >&2; exit 3; }
[ "${ID:-}" = omarchy ] || { echo "omaquery install: requires Omarchy on the remote machine" >&2; exit 3; }
printf 'arch=%s\n' "$(uname -m)"
if command -v omaquery >/dev/null 2>&1; then
  v=$(omaquery version 2>/dev/null) || v=
  v=${v#omaquery }
  printf 'version=%s\n' "$v"
fi`
	out, err := runInstallCommand(ctx, nil, "ssh", append(sshBaseArgs(target), script)...)
	if err != nil {
		return remoteInstallState{}, err
	}
	var state remoteInstallState
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		switch key {
		case "arch":
			state.Arch = value
		case "version":
			state.Version = value
		}
	}
	return state, nil
}

func archMatches(remote string) bool {
	switch runtime.GOARCH {
	case "amd64":
		return remote == "x86_64" || remote == "amd64"
	case "arm64":
		return remote == "aarch64" || remote == "arm64"
	default:
		return remote == runtime.GOARCH
	}
}

func compareVersions(a, b string) int {
	parse := func(value string) []int {
		value = strings.TrimPrefix(strings.TrimSpace(value), "v")
		parts := strings.Split(value, ".")
		nums := make([]int, 0, len(parts))
		for _, part := range parts {
			n, err := strconv.Atoi(part)
			if err != nil {
				return nil
			}
			nums = append(nums, n)
		}
		return nums
	}
	left, right := parse(a), parse(b)
	if left == nil || right == nil {
		return 0
	}
	for i := 0; i < len(left) || i < len(right); i++ {
		var l, r int
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		if l < r {
			return -1
		}
		if l > r {
			return 1
		}
	}
	return 0
}

func installRemote(ctx context.Context, target, executable string, force bool, out io.Writer) error {
	if !validTarget(target) {
		return errors.New("remote must be an SSH host or user@host without options")
	}
	if runtime.GOOS != "linux" {
		return errors.New("remote install requires a Linux build of omaquery")
	}
	state, err := probeRemote(ctx, target)
	if err != nil {
		return err
	}
	if state.Arch != "" && !archMatches(state.Arch) {
		return fmt.Errorf("remote machine is %s but this omaquery binary is %s; install a matching binary on the remote first", state.Arch, runtime.GOARCH)
	}
	if state.Version != "" && !force && compareVersions(state.Version, version) > 0 {
		return fmt.Errorf("remote omaquery %s is newer than %s; use --force to downgrade it", state.Version, version)
	}

	if state.Version == "" {
		fmt.Fprintf(out, "Installing omaquery %s on %s\n", version, target)
	} else {
		fmt.Fprintf(out, "Updating omaquery on %s (%s -> %s)\n", target, state.Version, version)
	}

	binary, err := os.Open(executable)
	if err != nil {
		return err
	}
	defer binary.Close()
	const script = `tmp=$(mktemp -d) || exit 1
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
cat > "$tmp/omaquery" || exit 1
chmod 700 "$tmp/omaquery" || exit 1
"$tmp/omaquery" install`
	cmd := remoteExec(ctx, "ssh", append(sshBaseArgs(target), script)...)
	cmd.Stdin = binary
	cmd.Stdout = out
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return fmt.Errorf("remote install failed: %s", detail)
		}
		return fmt.Errorf("remote install failed: %w", err)
	}
	if detail := strings.TrimSpace(stderr.String()); detail != "" {
		fmt.Fprintln(out, detail)
	}
	return nil
}
