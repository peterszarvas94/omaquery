// omaquery collects values through an Omarchy desktop prompt.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const maxMessage = 1 << 20
const help = `omaquery — request secrets through the Omarchy desktop

Usage:
  omaquery run [--reason TEXT] [--timeout 5m] [--prompt-on SSH_HOST] NAME[:hint] … -- COMMAND [ARG …]
  omaquery serve                 Private popup bridge (started by the plugin)
  omaquery version

Options must precede names. Timeout accepts Go durations or seconds and applies
only to the prompt, including queue time. Escape cancels. Values are passed only
to the child environment. Command output is forwarded unchanged: commands can
print their environment, so review the command before approving it.

Exit codes: 1 cancelled, 2 usage/setup/protocol error, 124 prompt timeout;
otherwise the command's exit status. No dotenv writing or 1Password integration.
`

type field struct {
	Name string `json:"name"`
	Hint string `json:"hint,omitempty"`
}
type request struct {
	ID      string   `json:"id,omitempty"`
	Type    string   `json:"type,omitempty"`
	Fields  []field  `json:"fields"`
	Command []string `json:"command"`
	Cwd     string   `json:"cwd"`
	Reason  string   `json:"reason,omitempty"`
	Origin  string   `json:"origin,omitempty"`
	Target  string   `json:"-"`
}
type response struct {
	ID        string            `json:"id,omitempty"`
	Cancelled bool              `json:"cancelled,omitempty"`
	Values    map[string]string `json:"values,omitempty"`
}

var namePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func main() { os.Exit(mainCode(os.Args[1:])) }
func mainCode(args []string) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "help" {
		fmt.Print(help)
		return 0
	}
	switch args[0] {
	case "version":
		fmt.Println("omaquery 0.2.0")
		return 0
	case "serve":
		if len(args) != 1 {
			return fail(errors.New("serve takes no arguments"))
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := serve(ctx, os.Stdin, os.Stdout); err != nil {
			return fail(err)
		}
		return 0
	case "run":
		return run(args[1:])
	case "relay":
		if len(args) != 1 {
			return fail(errors.New("relay takes no arguments"))
		}
		return relay(os.Stdin, os.Stdout)
	default:
		return fail(errors.New("unknown command; use omaquery --help"))
	}
}
func fail(err error) int { fmt.Fprintln(os.Stderr, "omaquery:", err); return 2 }
func parseRun(args []string) (request, time.Duration, error) {
	var req request
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	reason := fs.String("reason", "", "reason shown in the popup")
	timeout := fs.String("timeout", "5m", "prompt timeout")
	promptOn := fs.String("prompt-on", "", "SSH host on which to show the popup")
	if err := fs.Parse(args); err != nil {
		return req, 0, err
	}
	duration, err := time.ParseDuration(*timeout)
	if n, e := strconv.ParseFloat(*timeout, 64); e == nil {
		duration = time.Duration(n * float64(time.Second))
		err = nil
	}
	if err != nil || duration <= 0 || duration > 24*time.Hour {
		return req, 0, errors.New("timeout must be positive and at most 24h")
	}
	rest := fs.Args()
	sep := -1
	for i, v := range rest {
		if v == "--" {
			sep = i
			break
		}
	}
	if sep < 1 || sep == len(rest)-1 {
		return req, 0, errors.New("expected NAME[:hint] … -- COMMAND [ARG …] (options before names)")
	}
	seen := map[string]bool{}
	for _, spec := range rest[:sep] {
		name, hint, _ := strings.Cut(spec, ":")
		if !namePattern.MatchString(name) || seen[name] {
			return req, 0, errors.New("invalid or duplicate environment variable name")
		}
		seen[name] = true
		req.Fields = append(req.Fields, field{name, hint})
	}
	if len(req.Fields) > 16 {
		return req, 0, errors.New("at most 16 fields are supported")
	}
	req.Command = rest[sep+1:]
	req.Reason = *reason
	if *promptOn != "" && !validTarget(*promptOn) {
		return req, 0, errors.New("prompt-on must be an SSH host or user@host without options")
	}
	req.Target = *promptOn
	req.Cwd, err = os.Getwd()
	if err != nil {
		return req, 0, err
	}
	data, _ := json.Marshal(req)
	if len(data) > maxMessage/2 {
		return req, 0, errors.New("request too large")
	}
	return req, duration, nil
}
func run(args []string) int {
	if len(args) == 1 && args[0] == "--help" {
		fmt.Print(help)
		return 0
	}
	req, duration, err := parseRun(args)
	if err != nil {
		return fail(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	promptCtx, cancel := context.WithTimeout(ctx, duration)
	var values map[string]string
	if req.Target == "" {
		values, err = ask(promptCtx, req)
	} else {
		values, err = askRemote(promptCtx, req)
	}
	cancel()
	if errors.Is(err, context.DeadlineExceeded) {
		fmt.Fprintln(os.Stderr, "omaquery: prompt timed out")
		return 124
	}
	if errors.Is(err, context.Canceled) {
		return 130
	}
	if errors.Is(err, errCancelled) {
		fmt.Fprintln(os.Stderr, "omaquery: cancelled")
		return 1
	}
	if err != nil {
		return fail(err)
	}
	// Replace the wrapper so process managers and terminal signals reach the
	// command directly, and the wrapper does not retain a second secret copy.
	stop()
	path, err := exec.LookPath(req.Command[0])
	if err == nil {
		err = syscall.Exec(path, req.Command, environment(os.Environ(), values))
	}
	fmt.Fprintln(os.Stderr, "omaquery: cannot start command:", err)
	return 127
}
func environment(base []string, values map[string]string) []string {
	env := make([]string, 0, len(base)+len(values))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if _, ok := values[key]; !ok {
			env = append(env, entry)
		}
	}
	for key, value := range values {
		env = append(env, key+"="+value)
	}
	return env
}

var errCancelled = errors.New("cancelled")

func socketPath() (string, error) {
	runtime := os.Getenv("XDG_RUNTIME_DIR")
	if runtime == "" || !filepath.IsAbs(runtime) {
		return "", errors.New("XDG_RUNTIME_DIR must identify the local desktop session")
	}
	dir := filepath.Join(runtime, "omaquery")
	if err := os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
		return "", err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm() != 0700 || !ok || stat.Uid != uint32(os.Getuid()) {
		return "", errors.New("unsafe omaquery runtime directory (requires owned directory mode 0700)")
	}
	return filepath.Join(dir, "prompt.sock"), nil
}
func ask(ctx context.Context, req request) (map[string]string, error) {
	path, err := socketPath()
	if err != nil {
		return nil, err
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return nil, errors.New("popup unavailable; install and enable the Omarchy plugin")
	}
	defer conn.Close()
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-finished:
		}
	}()
	if err = json.NewEncoder(conn).Encode(req); err != nil {
		return nil, err
	}
	var res response
	err = json.NewDecoder(io.LimitReader(conn, maxMessage)).Decode(&res)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, errors.New("popup disconnected or sent an invalid response")
	}
	if res.Cancelled {
		return nil, errCancelled
	}
	if err = validateValues(req, res.Values); err != nil {
		return nil, err
	}
	return res.Values, nil
}
func validateValues(req request, values map[string]string) error {
	if len(values) != len(req.Fields) {
		return errors.New("popup returned the wrong number of values")
	}
	for _, f := range req.Fields {
		v, ok := values[f.Name]
		if !ok || v == "" || strings.ContainsRune(v, 0) {
			return errors.New("popup returned missing, empty, or invalid values")
		}
	}
	return nil
}

// serve owns the socket for the lifetime of the plugin's child process. flock
// prevents two bridges from unlinking each other's listening socket.
func serve(ctx context.Context, input io.Reader, output io.Writer) error {
	path, err := socketPath()
	if err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(filepath.Dir(path), "bridge.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("another popup bridge is already running")
	}
	if err = os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err = os.Chmod(path, 0600); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { <-ctx.Done(); listener.Close() }()
	replies := make(chan response)
	go func() {
		defer cancel()
		scanner := bufio.NewScanner(input)
		scanner.Buffer(make([]byte, 4096), maxMessage)
		for scanner.Scan() {
			var res response
			if json.Unmarshal(scanner.Bytes(), &res) != nil {
				return
			}
			select {
			case replies <- res:
			case <-ctx.Done():
				return
			}
		}
	}()
	gate := make(chan struct{}, 1)
	var sequence uint64
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		sequence++
		id := strconv.FormatUint(sequence, 10)
		go handle(ctx, conn, id, gate, replies, output)
	}
}
func handle(ctx context.Context, conn net.Conn, id string, gate chan struct{}, replies <-chan response, output io.Writer) {
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(io.LimitReader(conn, maxMessage))
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return
	}
	conn.SetReadDeadline(time.Time{})
	var req request
	if json.Unmarshal(line, &req) != nil || !validRequest(req) {
		return
	}
	disconnected := make(chan struct{})
	go func() { var b [1]byte; conn.Read(b[:]); close(disconnected) }()
	select {
	case gate <- struct{}{}:
	case <-ctx.Done():
		return
	case <-disconnected:
		return
	}
	defer func() { <-gate }()
	select {
	case <-disconnected:
		return
	case <-ctx.Done():
		return
	default:
	}
	req.ID = id
	req.Type = "request"
	encoder := json.NewEncoder(output)
	if encoder.Encode(req) != nil {
		return
	}
	defer encoder.Encode(struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}{"closed", id})
	for {
		select {
		case <-ctx.Done():
			return
		case <-disconnected:
			return
		case res := <-replies:
			if res.ID != id {
				continue
			}
			if !res.Cancelled && validateValues(req, res.Values) != nil {
				return
			}
			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			json.NewEncoder(conn).Encode(res)
			return
		}
	}
}
func validRequest(req request) bool {
	if len(req.Fields) == 0 || len(req.Fields) > 16 || len(req.Command) == 0 || req.Command[0] == "" || !filepath.IsAbs(req.Cwd) {
		return false
	}
	seen := map[string]bool{}
	for _, f := range req.Fields {
		if !namePattern.MatchString(f.Name) || seen[f.Name] {
			return false
		}
		seen[f.Name] = true
	}
	return true
}
