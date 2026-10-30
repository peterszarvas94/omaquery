package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

// This opt-in bridge is separate from the desktop/SSH bridge. HTTP is allowed
// only on an explicit tailnet address (or loopback for development), and only
// one explicitly allowed peer can use it. Tailscale supplies transport encryption
// and peer identity; this POC does not add application/device authentication.
type mobileEnvelope struct {
	Request request   `json:"request"`
	Expires time.Time `json:"expires_at"`
}
type mobilePending struct {
	mobileEnvelope
	reply    chan response
	sequence uint64
}
type mobileSnapshot struct {
	Hostname   string           `json:"hostname"`
	Requests   []mobileEnvelope `json:"requests"`
	DemoStatus string           `json:"demo_status,omitempty"`
}
type mobileBridge struct {
	mu         sync.Mutex
	pending    map[string]*mobilePending
	watchers   map[chan struct{}]bool
	hostname   string
	sequence   uint64
	demoStatus string
	demoActive bool
}

func newMobileBridge(hostname string) *mobileBridge {
	return &mobileBridge{pending: make(map[string]*mobilePending), watchers: make(map[chan struct{}]bool), hostname: hostname}
}
func (b *mobileBridge) changedLocked() {
	for ch := range b.watchers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
func (b *mobileBridge) add(env mobileEnvelope) (*mobilePending, error) {
	if !validRequest(env.Request) || !env.Expires.After(time.Now()) || env.Expires.After(time.Now().Add(24*time.Hour)) {
		return nil, errors.New("invalid or expired mobile request")
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	env.Request.ID = hex.EncodeToString(id[:])
	env.Request.Origin = b.hostname
	env.Request.Type = "request"
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.pending) >= 16 {
		return nil, errors.New("too many pending mobile requests")
	}
	// One SSE snapshot must remain within the phone's bounded message reader.
	payload, _ := json.Marshal(env)
	bytes := len(payload)
	for _, existing := range b.pending {
		data, _ := json.Marshal(existing.mobileEnvelope)
		bytes += len(data)
	}
	if bytes > maxMessage/2 {
		return nil, errors.New("mobile request queue is too large")
	}
	b.sequence++
	p := &mobilePending{mobileEnvelope: env, reply: make(chan response, 1), sequence: b.sequence}
	b.pending[env.Request.ID] = p
	b.changedLocked()
	return p, nil
}
func (b *mobileBridge) remove(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.pending[id]; ok {
		delete(b.pending, id)
		b.changedLocked()
	}
}
func (b *mobileBridge) snapshot() mobileSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	list := make([]*mobilePending, 0, len(b.pending))
	for _, p := range b.pending {
		if p.Expires.After(time.Now()) {
			list = append(list, p)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].sequence < list[j].sequence })
	snap := mobileSnapshot{Hostname: b.hostname, Requests: make([]mobileEnvelope, 0, len(list)), DemoStatus: b.demoStatus}
	for _, p := range list {
		snap.Requests = append(snap.Requests, p.mobileEnvelope)
	}
	return snap
}
func (b *mobileBridge) events(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unavailable", 500)
		return
	}
	ch := make(chan struct{}, 1)
	b.mu.Lock()
	if len(b.watchers) >= 8 {
		b.mu.Unlock()
		http.Error(w, "too many connections", http.StatusTooManyRequests)
		return
	}
	b.watchers[ch] = true
	b.mu.Unlock()
	defer func() { b.mu.Lock(); delete(b.watchers, ch); b.mu.Unlock() }()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		data, _ := json.Marshal(b.snapshot())
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, err := fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", data); err != nil {
			return
		}
		flusher.Flush()
		_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
		select {
		case <-r.Context().Done():
			return
		case <-ch:
		case <-ticker.C:
		}
	}
}
func (b *mobileBridge) answer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var res response
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxMessage))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&res) != nil || decoder.Decode(new(any)) != io.EOF || res.ID == "" {
		http.Error(w, "invalid response", 400)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	p, ok := b.pending[res.ID]
	if !ok || !p.Expires.After(time.Now()) {
		http.Error(w, "request is no longer pending", http.StatusGone)
		return
	}
	if (res.Cancelled && len(res.Values) != 0) || (!res.Cancelled && validateValues(p.Request, res.Values) != nil) {
		http.Error(w, "missing or invalid values", 400)
		return
	}
	// Consume exactly once, before unblocking the command. Replays are rejected.
	delete(b.pending, res.ID)
	p.reply <- res
	b.changedLocked()
	w.WriteHeader(http.StatusNoContent)
}
func (b *mobileBridge) demo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	b.mu.Lock()
	if b.demoActive {
		b.mu.Unlock()
		http.Error(w, "a demo is already pending", 409)
		return
	}
	b.demoActive = true
	b.demoStatus = "Waiting for approval"
	b.mu.Unlock()
	cwd, _ := os.Getwd()
	env := mobileEnvelope{Request: request{Fields: []field{{Name: "DEMO_TOKEN", Hint: "Use any dummy value — this is a simulated deployment"}}, Command: []string{"sh", "-c", "test -n \"$DEMO_TOKEN\" && printf 'Secret accepted. Demo deployment complete.\\n'"}, Cwd: cwd, Reason: "Try the Android approval flow with a dummy token"}, Expires: time.Now().Add(2 * time.Minute)}
	p, err := b.add(env)
	if err != nil {
		b.mu.Lock()
		b.demoActive = false
		b.mu.Unlock()
		http.Error(w, "cannot create demo", 503)
		return
	}
	go func() {
		ctx, cancel := context.WithDeadline(context.Background(), p.Expires)
		defer cancel()
		status := "Demo expired"
		select {
		case res := <-p.reply:
			if res.Cancelled {
				status = "Demo rejected"
			} else {
				cmd := exec.CommandContext(ctx, p.Request.Command[0], p.Request.Command[1:]...)
				cmd.Env = environment(os.Environ(), res.Values)
				cmd.Dir = p.Request.Cwd
				// No command output is captured, logged, or returned to the phone.
				if cmd.Run() == nil {
					status = "Secret accepted. Demo deployment complete."
				} else {
					status = "Demo command failed"
				}
			}
		case <-ctx.Done():
		}
		b.remove(p.Request.ID)
		b.mu.Lock()
		b.demoStatus = status
		b.demoActive = false
		b.changedLocked()
		b.mu.Unlock()
	}()
	w.WriteHeader(http.StatusAccepted)
}
func tailnetIP(ip netip.Addr) bool {
	return netip.MustParsePrefix("100.64.0.0/10").Contains(ip) || netip.MustParsePrefix("fd7a:115c:a1e0::/48").Contains(ip)
}
func mobileAddress(value string) (netip.Addr, error) {
	ip, err := netip.ParseAddr(value)
	if err != nil || (!tailnetIP(ip) && !ip.IsLoopback()) {
		return netip.Addr{}, errors.New("use an explicit Tailscale IP address (or loopback for development)")
	}
	return ip.Unmap(), nil
}
func (b *mobileBridge) handler(peer netip.Addr) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/events", b.events)
	mux.HandleFunc("/v1/response", b.answer)
	mux.HandleFunc("/v1/demo", b.demo)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		remote, e := netip.ParseAddr(host)
		// Do not trust forwarding headers, and do not expose CORS to websites.
		if err != nil || e != nil || remote.Unmap() != peer || r.Header.Get("Origin") != "" {
			http.Error(w, "peer not allowed", 403)
			return
		}
		mux.ServeHTTP(w, r)
	})
}
func mobileSocketPath() (string, error) {
	path, err := socketPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), "mobile.sock"), nil
}
func askMobile(ctx context.Context, req request) (map[string]string, error) {
	path, err := mobileSocketPath()
	if err != nil {
		return nil, err
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return nil, errors.New("mobile bridge unavailable; start omaquery mobile serve")
	}
	defer conn.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()
	deadline, ok := ctx.Deadline()
	if !ok {
		return nil, errors.New("mobile request requires a deadline")
	}
	if err = json.NewEncoder(conn).Encode(mobileEnvelope{req, deadline}); err != nil {
		return nil, err
	}
	var res response
	err = json.NewDecoder(io.LimitReader(conn, maxMessage)).Decode(&res)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, errors.New("mobile bridge disconnected or rejected the request")
	}
	if res.Cancelled {
		return nil, errCancelled
	}
	if err = validateValues(req, res.Values); err != nil {
		return nil, err
	}
	return res.Values, nil
}
func (b *mobileBridge) local(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	// Read only the first line; the next read detects caller exit/cancellation.
	line, err := bufio.NewReader(io.LimitReader(conn, maxMessage)).ReadBytes('\n')
	if err != nil {
		return
	}
	var env mobileEnvelope
	if json.Unmarshal(line, &env) != nil {
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	p, err := b.add(env)
	if err != nil {
		return
	}
	defer b.remove(p.Request.ID)
	disconnected := make(chan struct{})
	go func() { var one [1]byte; _, _ = conn.Read(one[:]); close(disconnected) }()
	timer := time.NewTimer(time.Until(p.Expires))
	defer timer.Stop()
	select {
	case res := <-p.reply:
		// If the caller already disconnected, do not send or execute anything.
		select {
		case <-disconnected:
			return
		case <-ctx.Done():
			return
		default:
		}
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_ = json.NewEncoder(conn).Encode(res)
	case <-disconnected:
	case <-timer.C:
	case <-ctx.Done():
	}
}
func serveMobile(ctx context.Context, address string, peer netip.Addr, out io.Writer) error {
	endpoint, err := netip.ParseAddrPort(address)
	if err != nil {
		return errors.New("listen must be an explicit IP:port")
	}
	ip, err := mobileAddress(endpoint.Addr().String())
	if err != nil {
		return err
	}
	if ip.IsLoopback() != peer.IsLoopback() {
		return errors.New("listen and allowed peer must both be tailnet addresses, or both loopback")
	}
	// The bind fails if this address is not present on this machine.
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	defer listener.Close()
	path, err := mobileSocketPath()
	if err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(filepath.Dir(path), "mobile.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("another mobile bridge is running")
	}
	if err = os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	local, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	defer local.Close()
	if err = os.Chmod(path, 0600); err != nil {
		return err
	}
	hostname, _ := os.Hostname()
	bridge := newMobileBridge(hostname)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	server := &http.Server{Handler: bridge.handler(peer), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() { <-ctx.Done(); server.Close(); local.Close() }()
	go func() {
		for {
			conn, err := local.Accept()
			if err != nil {
				return
			}
			go bridge.local(ctx, conn)
		}
	}()
	fmt.Fprintf(out, "Mobile bridge: http://%s\nAllowed peer: %s\nKeep the companion open with Tailscale connected.\n", address, peer)
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
func newMobileCommand() *cobra.Command {
	var address, peerValue string
	parent := &cobra.Command{Use: "mobile", Short: "Experimental Android companion bridge"}
	serve := &cobra.Command{Use: "serve", Short: "Serve pending requests to one Tailscale peer", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		peer, err := mobileAddress(strings.TrimSpace(peerValue))
		if err != nil {
			return fmt.Errorf("allow-peer: %w", err)
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return serveMobile(ctx, address, peer, cmd.OutOrStdout())
	}}
	serve.Flags().StringVar(&address, "listen", "127.0.0.1:9797", "Explicit Tailscale IP:port (loopback for development)")
	serve.Flags().StringVar(&peerValue, "allow-peer", "", "Only this Tailscale peer IP may connect")
	_ = serve.MarkFlagRequired("allow-peer")
	parent.AddCommand(serve)
	return parent
}
