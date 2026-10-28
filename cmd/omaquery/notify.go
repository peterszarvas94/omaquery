package main

import (
	"context"
	"os/exec"
	"time"
)

var sendNotification = notifyUser

// notifyUser is best-effort: notifications are a convenience and must never
// make install or uninstall fail.
func notifyUser(ctx context.Context, env []string, summary, body string) {
	notifyCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(notifyCtx, "notify-send", "--app-name=omaquery", "--urgency=low", summary, body)
	if env != nil {
		cmd.Env = env
	}
	_ = cmd.Run()
}
