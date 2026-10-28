package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseRun(t *testing.T) {
	req, d, err := parseRunCLI([]string{"--reason", "deploy", "--timeout", "30", "TOKEN:From settings", "REGION", "--", "echo", "a b"})
	if err != nil || d != 30*time.Second || req.Fields[0].Hint != "From settings" || req.Command[1] != "a b" {
		t.Fatalf("parse: %+v %v %v", req, d, err)
	}
	for _, args := range [][]string{
		{"KEY"}, {"KEY", "--"}, {"KEY", "KEY", "--", "true"}, {"BAD-NAME", "--", "true"},
		{"--timeout", "0", "KEY", "--", "true"}, {"--timeout", "NaN", "KEY", "--", "true"},
		{"--timeout", "25h", "KEY", "--", "true"},
	} {
		if _, _, err := parseRunCLI(args); err == nil {
			t.Fatalf("accepted invalid args %q", args)
		}
	}
}

func TestCobraHelpAndCommandBoundary(t *testing.T) {
	for _, test := range []struct {
		args []string
		want []string
	}{
		{[]string{"--help"}, []string{"Available Commands:", "install", "update", "uninstall", "run", "version", "help"}},
		{[]string{"run", "--help"}, []string{"--remote", "--reason", "--timeout", "NAME[:hint]"}},
	} {
		code := 0
		root := newRootCommand(&code)
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetArgs(test.args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		for _, want := range test.want {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("help %q missing %q: %s", test.args, want, out.String())
			}
		}
		if strings.Contains(out.String(), "  serve ") || strings.Contains(out.String(), "  relay ") {
			t.Fatalf("internal commands exposed in help: %s", out.String())
		}
	}
	req, _, err := parseRunCLI([]string{"TOKEN", "--", "sh", "-c", "printf '%s' \"$TOKEN\""})
	if err != nil || len(req.Command) != 3 || req.Command[2] != "printf '%s' \"$TOKEN\"" {
		t.Fatalf("command arguments changed: %+v %v", req, err)
	}
}

func parseRunCLI(args []string) (request, time.Duration, error) {
	var req request
	var duration time.Duration
	code := 0
	cmd := newRunCommand(func(r request, d time.Duration) int {
		req, duration = r, d
		return 0
	}, &code)
	cmd.SetArgs(args)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	return req, duration, err
}
func TestEnvironmentAndValidation(t *testing.T) {
	env := environment([]string{"TOKEN=old", "KEEP=yes", "TOKEN=duplicate"}, map[string]string{"TOKEN": "new=value\nline"})
	if strings.Join(env, "|") != "KEEP=yes|TOKEN=new=value\nline" {
		t.Fatal(env)
	}
	req := request{Fields: []field{{Name: "TOKEN"}}}
	for _, v := range []map[string]string{nil, {"OTHER": "x"}, {"TOKEN": ""}, {"TOKEN": "a\x00b"}, {"TOKEN": "x", "EXTRA": "y"}} {
		if validateValues(req, v) == nil {
			t.Fatal("accepted invalid response")
		}
	}
	if validateValues(req, map[string]string{"TOKEN": "a\nb='c'"}) != nil {
		t.Fatal("valid special characters rejected")
	}
}
func TestUnsafeRuntime(t *testing.T) {
	runtime := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtime)
	if err := os.Mkdir(filepath.Join(runtime, "omaquery"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := socketPath(); err == nil {
		t.Fatal("accepted public runtime directory")
	}
}

type bridgeHarness struct {
	input  *io.PipeWriter
	events chan request
	ctx    context.Context
}

func startBridge(t *testing.T) bridgeHarness {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	inputR, inputW := io.Pipe()
	outputR, outputW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, inputR, outputW) }()
	events := make(chan request, 32)
	go func() {
		dec := json.NewDecoder(outputR)
		for {
			var req request
			if dec.Decode(&req) != nil {
				return
			}
			events <- req
		}
	}()
	t.Cleanup(func() {
		cancel()
		inputW.Close()
		outputR.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("bridge did not stop")
		}
	})
	path, err := socketPath()
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		c, err := net.Dial("unix", path)
		if err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("bridge did not start")
		}
		time.Sleep(time.Millisecond * 5)
	}
	return bridgeHarness{inputW, events, ctx}
}
func (b bridgeHarness) event(t *testing.T, kind string) request {
	t.Helper()
	select {
	case req := <-b.events:
		if req.Type != kind {
			t.Fatalf("event type %q, want %q", req.Type, kind)
		}
		return req
	case <-time.After(3 * time.Second):
		t.Fatal("no popup event")
		return request{}
	}
}
func sampleRequest() request {
	return request{Fields: []field{{Name: "TOKEN"}}, Command: []string{"example", "a b"}, Cwd: "/tmp", Reason: "Test"}
}
func TestBridgeRoundTripAndCancel(t *testing.T) {
	b := startBridge(t)
	result := make(chan error, 1)
	go func() {
		v, err := ask(b.ctx, sampleRequest())
		if err == nil && v["TOKEN"] != "dummy\n'\"=value" {
			err = errors.New("value changed")
		}
		result <- err
	}()
	req := b.event(t, "request")
	if req.Command[1] != "a b" || req.Reason != "Test" {
		t.Fatal("request metadata lost")
	}
	json.NewEncoder(b.input).Encode(response{ID: req.ID, Values: map[string]string{"TOKEN": "dummy\n'\"=value"}})
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	b.event(t, "closed")
	go func() { _, err := ask(b.ctx, sampleRequest()); result <- err }()
	req = b.event(t, "request")
	json.NewEncoder(b.input).Encode(response{ID: req.ID, Cancelled: true})
	if err := <-result; !errors.Is(err, errCancelled) {
		t.Fatalf("cancel: %v", err)
	}
	b.event(t, "closed")
}
func TestBridgeTimeoutAndQueue(t *testing.T) {
	b := startBridge(t)
	first, cancel := context.WithCancel(b.ctx)
	result := make(chan error, 1)
	go func() { _, err := ask(first, sampleRequest()); result <- err }()
	old := b.event(t, "request")
	timed, stop := context.WithTimeout(b.ctx, 80*time.Millisecond)
	defer stop()
	_, err := ask(timed, sampleRequest())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued timeout: %v", err)
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	b.event(t, "closed")
	go func() { _, err := ask(b.ctx, sampleRequest()); result <- err }()
	next := b.event(t, "request")
	// Late responses must never satisfy a subsequent request.
	json.NewEncoder(b.input).Encode(response{ID: old.ID, Values: map[string]string{"TOKEN": "old"}})
	json.NewEncoder(b.input).Encode(response{ID: next.ID, Cancelled: true})
	if err := <-result; !errors.Is(err, errCancelled) {
		t.Fatal(err)
	}
	b.event(t, "closed")
}
func TestProcessHelper(t *testing.T) {
	raw := os.Getenv("OMAQUERY_TEST_ARGS")
	if raw == "" {
		return
	}
	var args []string
	json.Unmarshal([]byte(raw), &args)
	os.Exit(mainCode(args))
}
func TestCLICommandExitAndNoSecretOutput(t *testing.T) {
	b := startBridge(t)
	args, _ := json.Marshal([]string{"run", "TOKEN", "--", "sh", "-c", `test "$TOKEN" = 'dummy-value' || exit 99; printf 'child ran'; exit 7`})
	cmd := exec.Command(os.Args[0], "-test.run=^TestProcessHelper$")
	cmd.Env = append(os.Environ(), "OMAQUERY_TEST_ARGS="+string(args), "TOKEN=wrong")
	result := make(chan struct {
		output string
		err    error
	}, 1)
	go func() {
		out, err := cmd.CombinedOutput()
		result <- struct {
			output string
			err    error
		}{string(out), err}
	}()
	req := b.event(t, "request")
	json.NewEncoder(b.input).Encode(response{ID: req.ID, Values: map[string]string{"TOKEN": "dummy-value"}})
	r := <-result
	var exit *exec.ExitError
	if !errors.As(r.err, &exit) || exit.ExitCode() != 7 || r.output != "child ran" {
		t.Fatalf("child: %q %v", r.output, r.err)
	}
	b.event(t, "closed")
}
func TestBridgeRejectsInvalidRequest(t *testing.T) {
	b := startBridge(t)
	path, _ := socketPath()
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	json.NewEncoder(c).Encode(request{Fields: []field{{Name: "NOT-VALID"}}, Command: []string{"true"}, Cwd: "/tmp"})
	c.SetReadDeadline(time.Now().Add(time.Second))
	_, err = bufio.NewReader(c).ReadByte()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("invalid request not rejected: %v", err)
	}
	select {
	case <-b.events:
		t.Fatal("invalid request reached popup")
	default:
	}
}

func TestDuplicateBridgeDoesNotReplaceSocket(t *testing.T) {
	b := startBridge(t)
	if err := serve(b.ctx, strings.NewReader(""), io.Discard); err == nil {
		t.Fatal("second bridge acquired socket")
	}
	result := make(chan error, 1)
	go func() { _, err := ask(b.ctx, sampleRequest()); result <- err }()
	req := b.event(t, "request")
	json.NewEncoder(b.input).Encode(response{ID: req.ID, Cancelled: true})
	if err := <-result; !errors.Is(err, errCancelled) {
		t.Fatalf("original bridge broken: %v", err)
	}
	b.event(t, "closed")
}

func TestInvalidPopupResponseFailsClosed(t *testing.T) {
	b := startBridge(t)
	result := make(chan error, 1)
	go func() { _, err := ask(b.ctx, sampleRequest()); result <- err }()
	req := b.event(t, "request")
	json.NewEncoder(b.input).Encode(response{ID: req.ID, Values: map[string]string{"WRONG": "dummy"}})
	if err := <-result; err == nil {
		t.Fatal("invalid popup response accepted")
	}
	b.event(t, "closed")
}
