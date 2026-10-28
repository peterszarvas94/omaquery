package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUninstallLocalRemovesPluginAndBinary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	bundle, err := loadPluginBundle()
	if err != nil {
		t.Fatal(err)
	}
	pluginRoot := filepath.Join(home, "config", "omarchy", "plugins")
	dirs := []string{
		filepath.Join(pluginRoot, bundle.ID),
		filepath.Join(pluginRoot, ".omaquery.prompt.bak.20260101000000"),
	}
	for _, id := range legacyPluginIDs {
		dirs = append(dirs, filepath.Join(pluginRoot, id))
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	binaryPath := filepath.Join(home, ".local", "bin", "omaquery")
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath, []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}

	var calls []string
	stubInstallCommand(t, func(_ []string, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if name == "omarchy" && len(args) == 4 && args[0] == "plugin" && args[1] == "remove" {
			return nil, os.RemoveAll(filepath.Join(pluginRoot, args[2]))
		}
		return nil, nil
	})

	var out bytes.Buffer
	if err := uninstallLocal(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	for _, dir := range dirs {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("%s still exists: %v", dir, err)
		}
	}
	if _, err := os.Stat(binaryPath); !os.IsNotExist(err) {
		t.Fatalf("binary still exists: %v", err)
	}
	if !strings.Contains(out.String(), "Removed omaquery") {
		t.Fatalf("output: %s", out.String())
	}
	joined := strings.Join(calls, "\n")
	for _, id := range append([]string{bundle.ID}, legacyPluginIDs...) {
		if !strings.Contains(joined, "omarchy plugin remove "+id+" --yes") {
			t.Fatalf("missing removal for %s:\n%s", id, joined)
		}
	}
}

func TestUninstallRemote(t *testing.T) {
	source := filepath.Join(t.TempDir(), "omaquery")
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
		return exec.CommandContext(ctx, "sh", "-c", "cat >/dev/null; printf 'remote uninstall output\\n'")
	}
	remoteArch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH]
	if remoteArch == "" {
		t.Skip("unsupported test architecture")
	}
	stubInstallCommand(t, func(_ []string, name string, args ...string) ([]byte, error) {
		return []byte("arch=" + remoteArch + "\n"), nil
	})
	var out bytes.Buffer
	if err := uninstallRemote(context.Background(), "laptop", source, &out); err != nil {
		t.Fatal(err)
	}
	if uploads != 1 || !strings.Contains(out.String(), "Removing omaquery from laptop") || !strings.Contains(out.String(), "remote uninstall output") {
		t.Fatalf("uploads=%d output=%q", uploads, out.String())
	}
}

func TestUninstallLocalFallbackRestartsShell(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	bundle, err := loadPluginBundle()
	if err != nil {
		t.Fatal(err)
	}
	pluginDir := filepath.Join(home, "config", "omarchy", "plugins", bundle.ID)
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatal(err)
	}
	binaryPath := filepath.Join(home, ".local", "bin", "omaquery")
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath, []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}

	var calls []string
	stubInstallCommand(t, func(_ []string, name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		if strings.Contains(call, "plugin remove") {
			return nil, os.ErrPermission
		}
		return nil, nil
	})
	if err := uninstallLocal(context.Background(), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(calls, "\n"), "omarchy restart shell") {
		t.Fatalf("fallback did not restart shell:\n%s", strings.Join(calls, "\n"))
	}
}
