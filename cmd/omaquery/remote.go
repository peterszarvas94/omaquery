package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// A request uses one SSH process. There is no TCP listener in omaquery and no
// persistent remote connection. The peer's existing local popup bridge handles
// the request under the logged-in desktop user.
var remoteExec = exec.CommandContext
var heartbeatInterval = 2 * time.Second
var targetPattern = regexp.MustCompile(`^(?:[A-Za-z_][A-Za-z0-9_-]*@)?[A-Za-z0-9][A-Za-z0-9._-]*$`)

func validTarget(target string) bool { return len(target) <= 253 && targetPattern.MatchString(target) }

func sshArgs(target string) []string {
	return []string{
		"-T", "-oBatchMode=yes", "-oStrictHostKeyChecking=yes",
		"-oConnectTimeout=5", "-oConnectionAttempts=1",
		target, "~/.local/bin/omaquery relay",
	}
}

type relayResponse struct {
	Type      string            `json:"type"`
	Cancelled bool              `json:"cancelled,omitempty"`
	Values    map[string]string `json:"values,omitempty"`
	Error     string            `json:"error,omitempty"`
}

type cappedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.Len()
	if remaining > 0 {
		if remaining > len(p) {
			remaining = len(p)
		}
		_, _ = b.Buffer.Write(p[:remaining])
	}
	return len(p), nil
}

func askRemote(ctx context.Context, req request) (map[string]string, error) {
	if !validTarget(req.Target) {
		return nil, errors.New("invalid SSH target")
	}
	target := req.Target
	req.Target = ""
	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}
	req.Origin = hostname
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if len(payload) > maxMessage/2 {
		return nil, errors.New("request too large")
	}
	cmd := remoteExec(ctx, "ssh", sshArgs(target)...)
	cmd.Stdin = bytes.NewReader(append(payload, '\n'))
	stderr := &cappedBuffer{limit: 4096}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, fmt.Errorf("cannot start SSH: %w", err)
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), maxMessage)
	var res relayResponse
	gotResult := false
	for scanner.Scan() {
		if json.Unmarshal(scanner.Bytes(), &res) != nil {
			break
		}
		if res.Type == "keepalive" {
			continue
		}
		if res.Type == "result" {
			gotResult = true
		}
		break
	}
	if !gotResult && ctx.Err() == nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if !gotResult || scanner.Err() != nil {
		detail := strings.TrimSpace(stderr.String())
		if waitErr != nil && detail != "" {
			return nil, fmt.Errorf("SSH to %s failed: %s", target, detail)
		}
		return nil, errors.New("remote prompt returned an invalid response")
	}
	if res.Error != "" {
		return nil, fmt.Errorf("remote prompt: %s", res.Error)
	}
	if waitErr != nil {
		return nil, fmt.Errorf("SSH to %s failed: %s", target, strings.TrimSpace(stderr.String()))
	}
	if res.Cancelled {
		return nil, errCancelled
	}
	if err = validateValues(req, res.Values); err != nil {
		return nil, err
	}
	return res.Values, nil
}

// relay is an internal, request-scoped SSH protocol endpoint. SSH_CONNECTION
// supplies the authenticated network peer address; the hostname in the request
// is only a label supplied by the requesting process.
func relay(input io.Reader, output io.Writer) int {
	connection := strings.Fields(os.Getenv("SSH_CONNECTION"))
	if len(connection) != 4 || net.ParseIP(connection[0]) == nil {
		return fail(errors.New("relay requires an SSH session"))
	}
	if err := requireOmarchy(); err != nil {
		if encodeErr := json.NewEncoder(output).Encode(relayResponse{Type: "result", Error: err.Error()}); encodeErr != nil {
			return fail(encodeErr)
		}
		return 0
	}
	data, err := io.ReadAll(io.LimitReader(input, maxMessage+1))
	if err != nil || len(data) > maxMessage {
		return fail(errors.New("invalid or oversized relay request"))
	}
	var req request
	if json.Unmarshal(data, &req) != nil || !validRequest(req) {
		return fail(errors.New("invalid relay request"))
	}
	if len(req.Origin) > 255 {
		return fail(errors.New("invalid requesting host label"))
	}
	if req.Origin == "" {
		req.Origin = "unnamed host"
	}
	req.Origin = fmt.Sprintf("%s (SSH peer %s)", req.Origin, connection[0])
	req.ID = ""
	req.Type = ""
	req.Target = ""
	// Noninteractive SSH sessions do not consistently receive this variable.
	// The popup bridge always lives in the login user's systemd runtime dir.
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		_ = os.Setenv("XDG_RUNTIME_DIR", fmt.Sprintf("/run/user/%d", os.Getuid()))
	}
	parent, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	heartbeatDone := make(chan struct{})
	heartbeatStopped := make(chan struct{})
	go func() {
		defer close(heartbeatStopped)
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatDone:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := io.WriteString(output, "{\"type\":\"keepalive\"}\n"); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	values, err := ask(ctx, req)
	close(heartbeatDone)
	<-heartbeatStopped
	if ctx.Err() != nil {
		return fail(errors.New("remote requester disconnected"))
	}
	res := relayResponse{Type: "result", Values: values}
	switch {
	case errors.Is(err, errCancelled):
		res = relayResponse{Type: "result", Cancelled: true}
	case err != nil:
		res = relayResponse{Type: "result", Error: err.Error()}
	}
	if encodeErr := json.NewEncoder(output).Encode(res); encodeErr != nil {
		return fail(errors.New("remote requester disconnected"))
	}
	return 0
}
