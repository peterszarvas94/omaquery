package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mobileTestEnvelope() mobileEnvelope {
	return mobileEnvelope{Request: request{Fields: []field{{Name: "DEMO_TOKEN"}}, Command: []string{"./deploy", "--dry-run"}, Cwd: "/tmp", Reason: "Deploy demo"}, Expires: time.Now().Add(time.Minute)}
}
func mobilePOST(t *testing.T, b *mobileBridge, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "/v1/response", strings.NewReader(body))
	w := httptest.NewRecorder()
	b.answer(w, r)
	return w
}
func TestMobileResponseIsValidatedConsumedOnceAndNeverInSnapshot(t *testing.T) {
	b := newMobileBridge("desktop")
	p, err := b.add(mobileTestEnvelope())
	if err != nil {
		t.Fatal(err)
	}
	if w := mobilePOST(t, b, `{"id":"`+p.Request.ID+`","values":{"WRONG":"dummy"}}`); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if len(b.snapshot().Requests) != 1 {
		t.Fatal("invalid response consumed request")
	}
	value := "dummy-value-not-for-snapshot"
	payload := `{"id":"` + p.Request.ID + `","values":{"DEMO_TOKEN":"` + value + `"}}`
	if w := mobilePOST(t, b, payload); w.Code != 204 {
		t.Fatal(w.Code)
	}
	if res := <-p.reply; res.Values["DEMO_TOKEN"] != value {
		t.Fatal("value was not delivered")
	}
	snap, _ := json.Marshal(b.snapshot())
	if strings.Contains(string(snap), value) || len(b.snapshot().Requests) != 0 {
		t.Fatal("values leaked or request retained")
	}
	if w := mobilePOST(t, b, payload); w.Code != 410 {
		t.Fatal("replay accepted", w.Code)
	}
}
func TestMobileExpiryCancellationAndStrictJSON(t *testing.T) {
	b := newMobileBridge("desktop")
	p, _ := b.add(mobileTestEnvelope())
	if w := mobilePOST(t, b, `{"id":"`+p.Request.ID+`","cancelled":true,"values":{"DEMO_TOKEN":"dummy"}}`); w.Code != 400 {
		t.Fatal("cancel with values accepted")
	}
	if w := mobilePOST(t, b, `{"id":"`+p.Request.ID+`","cancelled":true} {}`); w.Code != 400 {
		t.Fatal("trailing json accepted")
	}
	if w := mobilePOST(t, b, `{"id":"`+p.Request.ID+`","cancelled":true}`); w.Code != 204 {
		t.Fatal(w.Code)
	}
	if !(<-p.reply).Cancelled {
		t.Fatal("not cancelled")
	}
	expired := mobileTestEnvelope()
	expired.Expires = time.Now().Add(-time.Second)
	if _, err := b.add(expired); err == nil {
		t.Fatal("expired request accepted")
	}
	p, _ = b.add(mobileTestEnvelope())
	p.Expires = time.Now().Add(-time.Second)
	if w := mobilePOST(t, b, `{"id":"`+p.Request.ID+`","values":{"DEMO_TOKEN":"dummy"}}`); w.Code != 410 {
		t.Fatal("expired approval accepted")
	}
}
func TestMobilePeerAndOriginRestrictions(t *testing.T) {
	b := newMobileBridge("desktop")
	h := b.handler(netip.MustParseAddr("100.64.0.2"))
	for _, tc := range []struct {
		peer, origin string
		want         int
	}{{"100.64.0.3:1234", "", 403}, {"100.64.0.2:1234", "https://evil.example.test", 403}, {"100.64.0.2:1234", "", 404}} {
		r := httptest.NewRequest("GET", "/unknown", nil)
		r.RemoteAddr = tc.peer
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("peer/origin: got %d want %d", w.Code, tc.want)
		}
	}
	for _, addr := range []string{"0.0.0.0", "192.168.1.2", "8.8.8.8", "example.test"} {
		if _, err := mobileAddress(addr); err == nil {
			t.Fatal("unsafe address accepted", addr)
		}
	}
}
func TestMobileSSEInitialSnapshotAndRequestClose(t *testing.T) {
	b := newMobileBridge("desktop")
	p, _ := b.add(mobileTestEnvelope())
	s := httptest.NewServer(http.HandlerFunc(b.events))
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", s.URL, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	reader := bufio.NewReader(res.Body)
	if line, _ := reader.ReadString('\n'); line != "event: snapshot\n" {
		t.Fatal(line)
	}
	line, _ := reader.ReadString('\n')
	if !strings.Contains(line, p.Request.ID) || !strings.Contains(line, "desktop") {
		t.Fatal("missing initial snapshot")
	}
	_, _ = reader.ReadString('\n')
	b.remove(p.Request.ID)
	line, _ = reader.ReadString('\n')
	if line != "event: snapshot\n" {
		t.Fatal(line)
	}
	line, _ = reader.ReadString('\n')
	if strings.Contains(line, p.Request.ID) {
		t.Fatal("closed request retained")
	}
}
func TestMobileCallerDisconnectRemovesPrompt(t *testing.T) {
	b := newMobileBridge("desktop")
	client, server := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.local(ctx, server)
	if err := json.NewEncoder(client).Encode(mobileTestEnvelope()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for len(b.snapshot().Requests) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(b.snapshot().Requests) != 1 {
		t.Fatal("no request")
	}
	client.Close()
	deadline = time.Now().Add(time.Second)
	for len(b.snapshot().Requests) > 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(b.snapshot().Requests) != 0 {
		t.Fatal("caller cancellation left pending prompt")
	}
}
func TestMobileUnixRoundTripAndPrivateSocket(t *testing.T) {
	runtime := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtime)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Reserve an ephemeral loopback port; serveMobile owns a separate socket.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	errs := make(chan error, 1)
	go func() { errs <- serveMobile(ctx, addr, netip.MustParseAddr("127.0.0.1"), io.Discard) }()
	path := filepath.Join(runtime, "omaquery", "mobile.sock")
	deadline := time.Now().Add(2 * time.Second)
	for {
		if info, e := os.Stat(path); e == nil {
			if info.Mode().Perm() != 0600 {
				t.Fatal("unsafe socket")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("bridge did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	requestCtx, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	result := make(chan error, 1)
	go func() {
		values, e := askMobile(requestCtx, mobileTestEnvelope().Request)
		if e == nil && values["DEMO_TOKEN"] != "dummy" {
			e = io.ErrUnexpectedEOF
		}
		result <- e
	}()
	resp, e := http.Get("http://" + addr + "/v1/events")
	if e != nil {
		t.Fatal(e)
	}
	scanner := bufio.NewScanner(resp.Body)
	var id string
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "data: ") {
			var snap mobileSnapshot
			if json.Unmarshal([]byte(strings.TrimPrefix(scanner.Text(), "data: ")), &snap) != nil {
				t.Fatal("invalid SSE")
			}
			if len(snap.Requests) > 0 {
				id = snap.Requests[0].Request.ID
				break
			}
		}
	}
	resp.Body.Close()
	if id == "" {
		t.Fatal("request not streamed")
	}
	answer, e := http.Post("http://"+addr+"/v1/response", "application/json", strings.NewReader(`{"id":"`+id+`","values":{"DEMO_TOKEN":"dummy"}}`))
	if e != nil {
		t.Fatal(e)
	}
	answer.Body.Close()
	if answer.StatusCode != 204 {
		t.Fatal(answer.StatusCode)
	}
	if e := <-result; e != nil {
		t.Fatal(e)
	}
	cancel()
	if e := <-errs; e != nil {
		t.Fatal(e)
	}
}

func TestMobileFlagAndRemoteAreExclusive(t *testing.T) {
	req, _, err := parseRunCLI([]string{"--mobile", "DEMO_TOKEN", "--", "true"})
	if err != nil || !req.Mobile {
		t.Fatalf("mobile option: %v", err)
	}
	if _, _, err := parseRunCLI([]string{"--mobile", "--remote", "laptop", "DEMO_TOKEN", "--", "true"}); err == nil {
		t.Fatal("combined mobile and SSH prompts")
	}
}

func TestMobileMetadataBudget(t *testing.T) {
	b := newMobileBridge("desktop")
	env := mobileTestEnvelope()
	env.Request.Reason = strings.Repeat("x", maxMessage/3)
	if _, err := b.add(env); err != nil {
		t.Fatal(err)
	}
	if _, err := b.add(env); err == nil {
		t.Fatal("oversized snapshot queue accepted")
	}
}
