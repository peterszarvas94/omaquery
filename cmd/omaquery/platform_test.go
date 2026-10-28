package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOmarchyRelease(t *testing.T) {
	for _, test := range []struct {
		content string
		want    bool
	}{
		{"NAME=Omarchy\nID=omarchy\n", true},
		{"ID=\"omarchy\"\n", true},
		{"ID=arch\nID_LIKE=omarchy\n", false},
		{"ID=omarchy-derived\n", false},
		{"NAME=Omarchy\n", false},
	} {
		if got := isOmarchyRelease(strings.NewReader(test.content)); got != test.want {
			t.Fatalf("release %q: got %v, want %v", test.content, got, test.want)
		}
	}
}

func TestNonOmarchyRejectsPromptAndRelay(t *testing.T) {
	previous := osReleasePath
	osReleasePath = filepath.Join(t.TempDir(), "os-release")
	t.Cleanup(func() { osReleasePath = previous })
	if err := os.WriteFile(osReleasePath, []byte("ID=arch\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := requireOmarchy(); !errors.Is(err, errRequiresOmarchy) {
		t.Fatalf("platform check: %v", err)
	}
	if code := mainCode([]string{"run", "TOKEN", "--", "true"}); code != 2 {
		t.Fatalf("run exit code: %d", code)
	}
	t.Setenv("SSH_CONNECTION", "192.0.2.5 40555 192.0.2.6 22")
	var output strings.Builder
	if code := relay(strings.NewReader(`{}`), &output); code != 0 {
		t.Fatalf("relay exit code: %d", code)
	}
	var result relayResponse
	if err := json.Unmarshal([]byte(output.String()), &result); err != nil || result.Type != "result" || !strings.Contains(result.Error, "Omarchy") {
		t.Fatalf("relay response: %+v, %v", result, err)
	}
}
