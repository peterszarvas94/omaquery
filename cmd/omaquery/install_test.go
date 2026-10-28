package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func stubInstallCommand(t *testing.T, handler func([]string, string, ...string) ([]byte, error)) {
	t.Helper()
	previous := runInstallCommand
	runInstallCommand = func(_ context.Context, env []string, name string, args ...string) ([]byte, error) {
		return handler(env, name, args...)
	}
	t.Cleanup(func() { runInstallCommand = previous })
}

func TestInstallLocalAndUpdate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	source := filepath.Join(home, "source")
	if err := os.WriteFile(source, []byte("binary-v0.5.0"), 0755); err != nil {
		t.Fatal(err)
	}

	var calls []string
	stubInstallCommand(t, func(_ []string, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if name == "omarchy" && len(args) == 3 && args[0] == "plugin" && args[1] == "list" {
			return []byte("[]"), nil
		}
		return nil, nil
	})

	var out bytes.Buffer
	if err := installLocal(context.Background(), source, &out); err != nil {
		t.Fatal(err)
	}
	bundle, err := loadPluginBundle()
	if err != nil {
		t.Fatal(err)
	}
	binaryPath := filepath.Join(home, ".local", "bin", "omaquery")
	if data, err := os.ReadFile(binaryPath); err != nil || string(data) != "binary-v0.5.0" {
		t.Fatalf("installed binary: %q %v", data, err)
	}
	if info, err := os.Stat(binaryPath); err != nil || info.Mode().Perm() != 0755 {
		t.Fatalf("binary mode: %v %v", info, err)
	}
	pluginDir := filepath.Join(home, "config", "omarchy", "plugins", bundle.ID)
	for _, name := range []string{"manifest.json", "Popup.qml"} {
		if _, err := os.Stat(filepath.Join(pluginDir, name)); err != nil {
			t.Fatalf("missing plugin %s: %v", name, err)
		}
	}
	joined := strings.Join(calls, "\n")
	for _, want := range []string{
		"omarchy plugin validate " + pluginDir,
		"omarchy-shell shell rescanPlugins",
		"omarchy plugin enable " + bundle.ID,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing call %q in:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "omarchy restart shell") {
		t.Fatal("fresh install restarted the shell")
	}
	if !strings.Contains(out.String(), "Installed omaquery 0.5.0") {
		t.Fatalf("output: %s", out.String())
	}

	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.json"), []byte(`{"id":"`+bundle.ID+`","version":"0.2.0"}`), 0644); err != nil {
		t.Fatal(err)
	}
	calls = nil
	stubInstallCommand(t, func(_ []string, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if name == "omarchy" && len(args) == 3 && args[0] == "plugin" && args[1] == "list" {
			return []byte(`[{"id":"` + bundle.ID + `","enabled":true}]`), nil
		}
		return nil, nil
	})
	out.Reset()
	if err := installLocal(context.Background(), source, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(calls, "\n"), "omarchy restart shell") {
		t.Fatal("update did not restart the shell")
	}
	if !strings.Contains(out.String(), "Updated omaquery 0.2.0 -> 0.5.0") {
		t.Fatalf("output: %s", out.String())
	}
}

func TestInstallEnvironmentKeepsDesktopSettings(t *testing.T) {
	path := t.TempDir()
	if err := os.MkdirAll(filepath.Join(path, "shell"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "shell", "shell.qml"), []byte("// fixture\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OMARCHY_PATH", path)
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("LANG", "C")
	t.Setenv("LC_ALL", "C")
	env, err := installEnvironment(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, entry := range env {
		key, value, found := strings.Cut(entry, "=")
		if found {
			values[key] = value
		}
	}
	if values["OMARCHY_PATH"] != path {
		t.Fatalf("OMARCHY_PATH: %q", values["OMARCHY_PATH"])
	}
	if values["XDG_RUNTIME_DIR"] != "/run/user/"+strconv.Itoa(os.Getuid()) {
		t.Fatalf("XDG_RUNTIME_DIR: %q", values["XDG_RUNTIME_DIR"])
	}
	if values["LANG"] != "C.UTF-8" || values["LC_ALL"] != "C.UTF-8" {
		t.Fatalf("locale: LANG=%q LC_ALL=%q", values["LANG"], values["LC_ALL"])
	}
}

func TestInstallLocalRemovesLegacyPlugin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	source := filepath.Join(home, "source")
	if err := os.WriteFile(source, []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}
	pluginRoot := filepath.Join(home, "config", "omarchy", "plugins")
	legacyDir := filepath.Join(pluginRoot, "omaquery.prompt")
	legacyBackup := filepath.Join(pluginRoot, ".omaquery.prompt.bak.20260101000000")
	for _, dir := range []string{legacyDir, legacyBackup} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "manifest.json"), []byte(`{"id":"omaquery.prompt","version":"0.3.0"}`), 0644); err != nil {
		t.Fatal(err)
	}

	var calls []string
	stubInstallCommand(t, func(_ []string, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if name == "omarchy" && len(args) == 4 && args[0] == "plugin" && args[1] == "remove" {
			return nil, os.RemoveAll(legacyDir)
		}
		if name == "omarchy" && len(args) == 3 && args[0] == "plugin" && args[1] == "list" {
			return []byte("[]"), nil
		}
		return nil, nil
	})
	var out bytes.Buffer
	if err := installLocal(context.Background(), source, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacyDir); !os.IsNotExist(err) {
		t.Fatalf("legacy plugin still exists: %v", err)
	}
	if _, err := os.Stat(legacyBackup); !os.IsNotExist(err) {
		t.Fatalf("legacy backup still exists: %v", err)
	}
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, "omarchy plugin remove omaquery.prompt --yes") {
		t.Fatalf("legacy removal missing:\n%s", joined)
	}
	if !strings.Contains(joined, "omarchy restart shell") {
		t.Fatal("migration did not restart the shell")
	}
}

func TestInstallLocalTakesOverPluginManagerCheckout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	source := filepath.Join(home, "source")
	if err := os.WriteFile(source, []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}
	bundle, err := loadPluginBundle()
	if err != nil {
		t.Fatal(err)
	}
	gitDir := filepath.Join(home, "config", "omarchy", "plugins", bundle.ID, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	stubInstallCommand(t, func(_ []string, name string, args ...string) ([]byte, error) {
		if name == "omarchy" && len(args) == 3 && args[0] == "plugin" && args[1] == "list" {
			return []byte("[]"), nil
		}
		return nil, nil
	})
	if err := installLocal(context.Background(), source, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(gitDir); !os.IsNotExist(err) {
		t.Fatalf("plugin manager checkout was not converted: %v", err)
	}
}

func TestInstallPluginBundle(t *testing.T) {
	bundle, err := loadPluginBundle()
	if err != nil {
		t.Fatal(err)
	}
	if bundle.ID != "io.github.peterszarvas94.omaquery" || bundle.Version != version {
		t.Fatalf("bundle: %+v, CLI version %s", bundle, version)
	}
	var manifest map[string]any
	if err := json.Unmarshal(bundle.Manifest, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest["version"] != version {
		t.Fatalf("manifest version %v, CLI version %s", manifest["version"], version)
	}
}

func TestCompareVersions(t *testing.T) {
	for _, test := range []struct {
		a, b string
		want int
	}{
		{"0.2.0", "0.3.0", -1},
		{"v1.2.3", "1.2.3", 0},
		{"1.10.0", "1.9.9", 1},
		{"2", "1.9", 1},
		{"bad", "1.0", 0},
	} {
		if got := compareVersions(test.a, test.b); got != test.want {
			t.Fatalf("compareVersions(%q, %q) = %d, want %d", test.a, test.b, got, test.want)
		}
	}
}

func TestArchMatches(t *testing.T) {
	want := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH]
	if want != "" && !archMatches(want) {
		t.Fatalf("%s did not match %s", want, runtime.GOARCH)
	}
	if archMatches("definitely-not-this-arch") {
		t.Fatal("unrelated architecture matched")
	}
}

func TestProbeRemote(t *testing.T) {
	stubInstallCommand(t, func(_ []string, name string, args ...string) ([]byte, error) {
		if name != "ssh" {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		return []byte("arch=x86_64\nversion=0.2.0\n"), nil
	})
	state, err := probeRemote(context.Background(), "laptop")
	if err != nil {
		t.Fatal(err)
	}
	if state.Arch != "x86_64" || state.Version != "0.2.0" {
		t.Fatalf("state: %+v", state)
	}
}

func TestInstallRemoteOlderAndNewer(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(source, []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}
	previousRemoteExec := remoteExec
	t.Cleanup(func() { remoteExec = previousRemoteExec })
	uploads := 0
	remoteExec = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		uploads++
		if name != "ssh" {
			t.Fatalf("unexpected remote command: %s %v", name, args)
		}
		return exec.CommandContext(ctx, "sh", "-c", "cat >/dev/null; printf 'remote install output\\n'")
	}

	remoteArch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH]
	if remoteArch == "" {
		t.Skip("unsupported test architecture")
	}
	stubInstallCommand(t, func(_ []string, name string, args ...string) ([]byte, error) {
		return []byte("arch=" + remoteArch + "\nversion=0.2.0\n"), nil
	})
	var out bytes.Buffer
	if err := installRemote(context.Background(), "laptop", source, false, &out); err != nil {
		t.Fatal(err)
	}
	if uploads != 1 || !strings.Contains(out.String(), "0.2.0 -> 0.5.0") || !strings.Contains(out.String(), "remote install output") {
		t.Fatalf("uploads=%d output=%q", uploads, out.String())
	}

	stubInstallCommand(t, func(_ []string, name string, args ...string) ([]byte, error) {
		return []byte("arch=" + remoteArch + "\nversion=9.0.0\n"), nil
	})
	out.Reset()
	if err := installRemote(context.Background(), "laptop", source, false, &out); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("newer remote: %v", err)
	}
	if err := installRemote(context.Background(), "laptop", source, true, &out); err != nil {
		t.Fatalf("forced newer remote: %v", err)
	}
}
