# Android companion — notification preview

A native Android app with a user-enabled background SSE listener. Real
`omaquery run --mobile` requests appear as Android notifications. Tapping an
alert opens a dedicated approval screen for that exact request; values are
returned by HTTP POST. The normal desktop popup and SSH relay stay separate.

## Use it

1. Connect the computer and phone to Tailscale. Build the CLI with `make build`
   and run a bridge on the computer's tailnet IP, allowing only the phone:

   ```sh
   build/omaquery mobile serve --listen 100.100.10.10:9797 --allow-peer 100.100.10.11
   ```

2. Install the APK over the previous preview. Set the computer address to
   `100.100.10.10:9797` or `laptop:9797`, tap **Connect**, and grant Android's
   notification permission. A persistent listening notification stays visible.
3. Leave the app. On the computer, request values for an actual command:

   ```sh
   build/omaquery run --mobile --reason 'Deploy the release' DEPLOY_TOKEN -- ./deploy
   ```

4. Tap the new **A command needs your approval** notification. Review the host,
   exact command, working directory, reason and deadline. Enter the value and
   choose **Run command** or **Cancel**.

The connection screen lists pending requests as a fallback. It has no demo
button. Only the waiting CLI invocation gets approved values. `--timeout 5m`
controls expiry; stopping the CLI withdraws the request and its notification.
An alert for one request can never silently show another queued command.

Use **Disconnect** in the app, or **Stop listening** on its persistent
notification, to stop the background connection. After reboot, reopen the app
to resume listening. Force-stopping the app prevents notifications until it is
opened again. Keep Tailscale connected on both devices.

## Background delivery and Android

The app starts a user-visible foreground service while its connection screen is
open. Android 13+ notification permission is requested explicitly. Android 14+
uses the `specialUse` foreground service type for the user-requested ongoing
approval connection. Incoming notifications open an activity through a direct,
immutable PendingIntent with a normal back stack; no notification trampoline,
full-screen takeover, or overlay permission is used.

This is a sideload preview, not a claim of Play Store foreground-service policy
approval. The listener reconnects with bounded backoff. It does not use Firebase,
a public push relay, polling jobs, or a permanent CPU wake lock. Doze/network
suspension and vendor battery management can still delay delivery. On devices
that kill the listener, allow background operation in the phone's battery
settings. There is no automatic boot receiver.

## Security boundaries

- The bridge binds to one explicit tailnet IP and accepts one exact phone IP.
  Wildcard/LAN binds, forwarding-header trust and browser CORS are not supported.
  Loopback is allowed only for local development.
- Tailscale supplies network encryption and peer identity. There is no separate
  app pairing, app credential or biometric gate. Other software originating
  traffic from the allowed device can also reach the bridge. Use trusted devices
  and restrict this port in tailnet policy. HTTP has no independent TLS layer.
- Requests and values remain in memory. Notifications and PendingIntents contain
  generic wording and a request ID, never entered values or command contents.
  Expired, cancelled and consumed requests have their alerts withdrawn.
- The background service handles request metadata only. Secret entry lives in
  the secure approval activity, is cleared on pause, and is sent only to the
  approved command. Values are not written to preferences, backups, logs,
  ordinary CLI output or command arguments. The approved command itself can
  print or transmit its environment, so review it before approval.
- The local mobile socket is separate, owned, and mode 0600 inside an owned
  mode-0700 runtime directory. One-shot replies reject replays and stale IDs.
- Android secure-window protection, disabled input autofill/view saving and
  backup exclusions remain enabled. Use a trusted keyboard; these measures do
  not protect against root or an untrusted operating system.

## Build and verify

Requires JDK 21+, platform 36, build-tools 36.0.0, `zip`, and an existing standard
Android development keystore. No Gradle, Android libraries or Python are needed:

```sh
export ANDROID_HOME=/path/to/android-sdk
export JAVA_HOME=/path/to/jdk-21
./android/build-apk.sh
```

Output: `android/build/omaquery-0.2.1.apk`. Version code 3 updates the previous
preview when signed with the same key. The script reuses `~/.android/debug.keystore`
or `ANDROID_DEBUG_KEYSTORE`; this is development signing, not a release key.
`OMAQUERY_BRIDGE_URL` can prefill a non-secret literal tailnet IPv4 address.
The connection field also accepts short MagicDNS names and tailnet FQDNs, with
or without `http://`. Tailscale DNS must be enabled for hostname resolution.
DNS runs off the main thread and all answers must be in the tailnet address
ranges; HTTP connections and approval POSTs use the validated resolved IP. API
and build-tools versions can be overridden with `SDK_PLATFORM` and `BUILD_TOOLS`.

Run `make check`, Android SDK standalone lint, APK signature verification and
manual Android notification/tap/approval checks. No frontend UI tests are included.
The old bridge's `/v1/demo` endpoint remains for backwards compatibility but the
notification app never calls it. Request/response protocol is unchanged:
`GET /v1/events` snapshots and `POST /v1/response` one-shot replies.
