package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func latestServer(t *testing.T, version string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"Version": version})
	}))
	t.Cleanup(server.Close)
	previous := latestVersionURL
	latestVersionURL = server.URL
	t.Cleanup(func() { latestVersionURL = previous })
	return server
}

func TestLatestVersion(t *testing.T) {
	latestServer(t, "v9.9.9")
	got, err := latestVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "v9.9.9" {
		t.Fatalf("version: %q", got)
	}
}

func TestUpdateRunsLatestInstall(t *testing.T) {
	latestServer(t, "v9.9.9")
	previous := runLatestInstall
	var gotLatest, gotRemote string
	var gotForce bool
	runLatestInstall = func(_ context.Context, latest, remote string, force bool, _, _ io.Writer) error {
		gotLatest, gotRemote, gotForce = latest, remote, force
		return nil
	}
	t.Cleanup(func() { runLatestInstall = previous })

	var out, errOut bytes.Buffer
	if err := update(context.Background(), "", false, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if gotLatest != "v9.9.9" || gotRemote != "" || gotForce {
		t.Fatalf("latest=%q remote=%q force=%v", gotLatest, gotRemote, gotForce)
	}
	if !strings.Contains(out.String(), "Updating omaquery "+version+" -> v9.9.9") {
		t.Fatalf("output: %s", out.String())
	}
}

func TestUpdateAlreadyCurrent(t *testing.T) {
	latestServer(t, "v"+version)
	previous := runLatestInstall
	runLatestInstall = func(context.Context, string, string, bool, io.Writer, io.Writer) error {
		return errors.New("must not reinstall local current version")
	}
	t.Cleanup(func() { runLatestInstall = previous })

	var out bytes.Buffer
	if err := update(context.Background(), "", false, &out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Already on latest omaquery "+version) {
		t.Fatalf("output: %s", out.String())
	}
}
