package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSSHArgumentsAndTargets(t *testing.T) {
	for _, target := range []string{"desktop", "user@desktop", "desktop.tail.example.com", "192.0.2.6"} {
		if !validTarget(target) {
			t.Fatalf("valid SSH target rejected: %q", target)
		}
	}
	for _, target := range []string{"-oProxyCommand=evil", "foo bar", "foo;rm", "x@", "@host", "host/dir", "user@@host", "host:22", ""} {
		if validTarget(target) {
			t.Fatalf("unsafe SSH target accepted: %q", target)
		}
	}
	want := []string{"-T", "-oBatchMode=yes", "-oStrictHostKeyChecking=yes", "-oConnectTimeout=5", "-oConnectionAttempts=1", "desktop", "~/.local/bin/omaquery relay"}
	if got := sshArgs("desktop"); !reflect.DeepEqual(got, want) {
		t.Fatalf("ssh args: %q", got)
	}
	req, _, err := parseRunCLI([]string{"--prompt-on", "desktop", "TOKEN", "--", "true"})
	if err != nil || req.Target != "desktop" {
		t.Fatalf("remote parse: %+v %v", req, err)
	}
	if _, _, err = parseRunCLI([]string{"--prompt-on", "-oProxyCommand=evil", "TOKEN", "--", "true"}); err == nil {
		t.Fatal("unsafe target accepted by CLI")
	}
}

func TestRemoteProcessHelper(t *testing.T) {
	if os.Getenv("OMAQUERY_TEST_REMOTE_HELPER") == "" {
		return
	}
	if os.Getenv("OMAQUERY_TEST_HEARTBEAT_MS") != "" {
		heartbeatInterval = 10 * time.Millisecond
	}
	if os.Getenv("OMAQUERY_TEST_REMOTE_HELPER") == "hang" {
		time.Sleep(5 * time.Second)
		return
	}
	os.Exit(relay(os.Stdin, os.Stdout))
}

func fakeSSH(t *testing.T, mode string) {
	t.Helper()
	previous := remoteExec
	remoteExec = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRemoteProcessHelper$")
		cmd.Env = append(os.Environ(), "OMAQUERY_TEST_REMOTE_HELPER="+mode, "OMAQUERY_TEST_HEARTBEAT_MS=10", "SSH_CONNECTION=192.0.2.5 40555 192.0.2.6 22")
		return cmd
	}
	t.Cleanup(func() { remoteExec = previous })
}

func TestRemoteRoundTripAndCancellation(t *testing.T) {
	b := startBridge(t)
	fakeSSH(t, "relay")
	req := sampleRequest()
	req.Target = "desktop"
	result := make(chan struct {
		values map[string]string
		err    error
	}, 1)
	go func() {
		values, err := askRemote(b.ctx, req)
		result <- struct {
			values map[string]string
			err    error
		}{values, err}
	}()
	popup := b.event(t, "request")
	if !strings.Contains(popup.Origin, "SSH peer 192.0.2.5") || popup.Target != "" {
		t.Fatalf("remote origin not labeled: %+v", popup)
	}
	if popup.Command[0] != "example" || popup.Cwd != "/tmp" {
		t.Fatal("source command metadata changed")
	}
	// The requester must tolerate heartbeats before the user submits.
	time.Sleep(50 * time.Millisecond)
	json.NewEncoder(b.input).Encode(response{ID: popup.ID, Values: map[string]string{"TOKEN": "dummy-secret"}})
	got := <-result
	if got.err != nil || got.values["TOKEN"] != "dummy-secret" {
		t.Fatalf("remote round trip: %+v", got)
	}
	b.event(t, "closed")
	go func() {
		values, err := askRemote(b.ctx, req)
		result <- struct {
			values map[string]string
			err    error
		}{values, err}
	}()
	popup = b.event(t, "request")
	json.NewEncoder(b.input).Encode(response{ID: popup.ID, Cancelled: true})
	got = <-result
	if !errors.Is(got.err, errCancelled) {
		t.Fatalf("remote cancel: %+v", got)
	}
	b.event(t, "closed")
}

func TestRemoteTimeoutClosesSSH(t *testing.T) {
	fakeSSH(t, "hang")
	req := sampleRequest()
	req.Target = "desktop"
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	_, err := askRemote(ctx, req)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("remote timeout: %v", err)
	}
}

func TestRelayRequiresSSHAndValidRequest(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "")
	var out strings.Builder
	if code := relay(strings.NewReader(`{}`), &out); code != 2 || out.Len() != 0 {
		t.Fatalf("local relay accepted: %d %q", code, out.String())
	}
	t.Setenv("SSH_CONNECTION", "192.0.2.5 456 192.0.2.6 22")
	if code := relay(strings.NewReader(`{}`), io.Discard); code != 2 {
		t.Fatalf("invalid request accepted: %d", code)
	}
}

type failedWriter struct{}

func (failedWriter) Write(p []byte) (int, error) { return 0, io.ErrClosedPipe }

func TestRelayDisconnectClosesPopup(t *testing.T) {
	b := startBridge(t)
	t.Setenv("SSH_CONNECTION", "192.0.2.5 40555 192.0.2.6 22")
	previous := heartbeatInterval
	heartbeatInterval = 10 * time.Millisecond
	t.Cleanup(func() { heartbeatInterval = previous })
	req := sampleRequest()
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 1)
	go func() { done <- relay(strings.NewReader(string(payload)), failedWriter{}) }()
	b.event(t, "request")
	select {
	case code := <-done:
		if code != 2 {
			t.Fatalf("disconnect exit: %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("relay ignored disconnect")
	}
	b.event(t, "closed")
}
