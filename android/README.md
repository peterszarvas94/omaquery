# Android companion proof of concept

A small native Android app, plus an explicitly enabled Go mobile bridge. It uses
SSE snapshots for pending requests and HTTP POST for approvals and rejection.
The normal Omarchy popup and SSH relay remain separate.

## Try it

1. Connect the computer and Android phone to Tailscale.
2. Build the CLI with `make build`, then start a bridge on the computer's
   Tailscale IP, allowing **only the phone's Tailscale IP**:

   ```sh
   build/omaquery mobile serve --listen 100.100.10.10:9797 --allow-peer 100.100.10.11
   ```

3. Sideload `omaquery-companion.apk`. Set its computer address to
   `http://100.100.10.10:9797`, tap **Connect**, and keep it open.
4. Tap **Try a demo request**, review the exact command, enter a dummy value,
   and tap **Approve and run**. A fixed simulated deployment command executes
   on the computer with the value in its environment. Its result appears on the
   phone; the value itself is never printed.
5. For your own commands, use the newly built CLI:

   ```sh
   build/omaquery run --mobile --reason 'Deploy the demo' DEPLOY_TOKEN -- ./deploy
   ```

Only that invocation receives the approved values. Rejection exits without
running the command. `--timeout 5m` controls expiration; stopping the waiting
CLI removes its pending request. `--mobile` and `--remote` cannot be combined.
Stop the bridge with Ctrl+C. Do not substitute the normal installed CLI unless
it has been rebuilt with this branch.

## Boundaries

- **Foreground only:** no Android notification, foreground service, Firebase,
  or wake-up mechanism. Going to the background closes SSE and clears entered
  values. Returning reconnects to a fresh snapshot. Pending requests can survive
  phone disconnection until the CLI cancels or its deadline passes.
- **Tailnet only:** the listener accepts an explicit Tailscale IP and one exact
  peer IP. No wildcard or LAN address, forwarding headers, CORS, or public
  listener. Loopback is supported for development only.
- **Network identity, not app identity:** every app/process able to originate
  traffic from that allowed device can access the bridge. There is no separate
  pairing, app credential, biometric check, or per-command policy in this POC.
  Run it only with a trusted phone and trusted local user. Tailnet access policy
  should also restrict this port. This is not a production secret manager.
- HTTP is carried inside the encrypted Tailscale tunnel. It does not implement
  independent HTTPS. The Android address field accepts literal tailnet IPv4
  addresses only (plus `10.0.2.2` in this debug preview for an emulator).
- Requests and values stay in memory. SSE carries request metadata and public
  demo status only. Replies are one-shot and expire. Values are excluded from
  app preferences, backups, logs, command arguments and ordinary CLI output.
  The approved command itself can print or transmit its environment.
- Android uses secure-window protection and disables input autofill and saved
  view state. A keyboard still necessarily receives what you type; use a trusted
  keyboard. These measures do not protect against root or an untrusted OS.
- The CLI's local mobile socket is separate, owned, and mode 0600 in an owned
  mode-0700 runtime directory. The existing desktop bridge remains unaffected.

## Build the APK

Requires an Android SDK with platform 36 and build-tools 36.0.0, JDK 21+, `zip`,
and an existing standard Android development keystore. No Gradle, third-party
Android dependencies, embedded credentials, or Python are needed:

```sh
export ANDROID_HOME=/path/to/android-sdk
export JAVA_HOME=/path/to/jdk-21
./android/build-apk.sh
```

The script reuses `~/.android/debug.keystore`, or `ANDROID_DEBUG_KEYSTORE`.
The standard development key uses Android's conventional debug alias/password;
this sideload preview must not be published as a release build. Output:
`android/build/omaquery-companion.apk`.

Optionally set `OMAQUERY_BRIDGE_URL=http://100.100.10.10:9797` to prefill a non-secret
connection address in the APK. Never embed a token or secret. API and build-tools
versions can be overridden with `SDK_PLATFORM` and `BUILD_TOOLS`.

Go validation: `make check`. Android validation: the SDK's standalone `lint`
with this app's sources, resources and compiled classes, followed by manual
emulator/device checks. No Android UI tests are included.

## Protocol

- `GET /v1/events`: `event: snapshot` containing `{hostname, requests,
  demo_status?}`. Each request has a `request` object and an `expires_at` RFC3339
  deadline. The initial snapshot is sent immediately; subsequent snapshots also
  serve as heartbeats. Reconnect fetches all still-pending requests.
- `POST /v1/response`: `{id, cancelled, values?}`. Values must match all field
  names and be non-empty. HTTP 204 accepts; 400 rejects malformed values; 410
  rejects expired, cancelled, consumed, and unknown IDs. The request is consumed
  before its command resumes. A lost response acknowledgement must not cause a
  second execution.
- `POST /v1/demo`: creates one fixed, two-minute demo request (HTTP 202). It does
  not accept caller-provided commands. Additional simultaneous demos get 409.

The bridge limits pending requests, aggregate metadata size, HTTP body/header
size, and simultaneous SSE connections. It does not log request/response bodies.
