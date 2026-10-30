package com.omaquery.companion;

import android.app.Activity;
import android.content.SharedPreferences;
import android.graphics.Color;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.text.InputType;
import android.view.View;
import android.view.WindowInsets;
import android.view.WindowManager;
import android.view.inputmethod.InputMethodManager;
import android.widget.Button;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;
import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URI;
import java.nio.charset.StandardCharsets;
import java.time.Instant;
import java.util.ArrayList;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.atomic.AtomicInteger;
import org.json.JSONArray;
import org.json.JSONObject;

/** Foreground-only POC. Values never enter preferences, notifications or logs. */
public final class MainActivity extends Activity {
  private static final int BG = Color.rgb(18, 20, 28), CARD = Color.rgb(29, 32, 45),
                           FG = Color.rgb(239, 239, 248), MUTED = Color.rgb(168, 174, 196),
                           ACCENT = Color.rgb(194, 181, 255), GREEN = Color.rgb(133, 221, 178);
  private final Handler ui = new Handler(Looper.getMainLooper());
  private final ExecutorService posts = Executors.newSingleThreadExecutor();
  private final AtomicInteger generation = new AtomicInteger();
  private final ArrayList<EditText> fields = new ArrayList<>();
  private final ArrayList<String> names = new ArrayList<>();
  private LinearLayout root, content;
  private TextView status, expiry;
  private EditText address;
  private Button connect, approve, reject;
  private volatile HttpURLConnection stream;
  private volatile String base = "";
  private String currentId = "", hostname = "";
  private long expires;
  private boolean active = false, wanted = false, submitting = false;
  private final Runnable clock = new Runnable() {
    public void run() {
      if (expiry != null && !currentId.isEmpty()) {
        long left = Math.max(0, (expires - System.currentTimeMillis() + 999) / 1000);
        expiry.setText(left == 0 ? "Request expired" : "Expires in " + left + "s");
        if (left == 0) {
          clearValues();
          if (approve != null)
            approve.setEnabled(false);
          if (reject != null)
            reject.setEnabled(false);
        }
      }
      if (active)
        ui.postDelayed(this, 1000);
    }
  };

  @Override
  public void onCreate(Bundle state) {
    super.onCreate(state);
    // Prevent Android from persisting text input when the activity is recreated.
    getWindow().setFlags(
        WindowManager.LayoutParams.FLAG_SECURE, WindowManager.LayoutParams.FLAG_SECURE);
    ScrollView scroll = new ScrollView(this);
    scroll.setFillViewport(true);
    scroll.setBackgroundColor(BG);
    scroll.setSaveEnabled(false);
    root = new LinearLayout(this);
    root.setOrientation(LinearLayout.VERTICAL);
    root.setPadding(dp(22), dp(20), dp(22), dp(24));
    root.setImportantForAutofill(View.IMPORTANT_FOR_AUTOFILL_NO_EXCLUDE_DESCENDANTS);
    scroll.addView(root);
    if (Build.VERSION.SDK_INT >= 30) {
      scroll.setOnApplyWindowInsetsListener((v, insets) -> {
        android.graphics.Insets i =
            insets.getInsets(WindowInsets.Type.systemBars() | WindowInsets.Type.ime());
        root.setPadding(dp(22) + i.left, dp(20) + i.top, dp(22) + i.right, dp(24) + i.bottom);
        return insets;
      });
    } else {
      scroll.setFitsSystemWindows(true);
    }
    setContentView(scroll);
    add(root, "OMAQUERY / ANDROID PREVIEW", 12, ACCENT, false);
    TextView title = add(root, "Approve on your phone.", 29, FG, true);
    margin(title, 12, 6);
    TextView intro = add(
        root, "Review the command. Enter its values. You decide when it runs.", 16, MUTED, false);
    margin(intro, 0, 22);
    LinearLayout connection = card(root);
    add(connection, "YOUR COMPUTER", 12, ACCENT, true);
    address = new EditText(this);
    address.setSingleLine(true);
    address.setTextColor(FG);
    address.setTextSize(16);
    address.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_URI);
    address.setHint("http://100.x.y.z:9797");
    address.setHintTextColor(MUTED);
    address.setSaveEnabled(false);
    SharedPreferences prefs = getSharedPreferences("connection", MODE_PRIVATE);
    address.setText(prefs.getString("address", BuildConfig.DEFAULT_BRIDGE));
    connection.addView(address, wide());
    connect = button(connection, "Connect", true);
    connect.setOnClickListener(v -> {
      if (wanted) {
        wanted = false;
        disconnect();
        connectionState("Disconnected", MUTED);
        connect.setText(R.string.connect);
        address.setEnabled(true);
        showIdle("Connect to your computer", "Turn on Tailscale on both devices, then connect.");
      } else {
        try {
          base = validateAddress(address.getText().toString());
          prefs.edit().putString("address", base).apply();
          wanted = true;
          address.setEnabled(false);
          connect.setText(R.string.disconnect);
          startStream();
        } catch (Exception e) {
          address.setError("Use your computer’s Tailscale IP and port, e.g. http://100.x.y.z:9797");
        }
      }
    });
    status = add(connection, "Not connected", 14, MUTED, false);
    margin(status, 10, 2);
    TextView note = add(root,
        "Keep this app open and Tailscale connected. Background notifications are not included in "
        + "this preview.",
        13, MUTED, false);
    margin(note, 14, 20);
    content = new LinearLayout(this);
    content.setOrientation(LinearLayout.VERTICAL);
    root.addView(content, wide());
    showIdle("Ready when you are", "Connect to your computer to see pending requests.");
  }
  @Override
  protected void onResume() {
    super.onResume();
    active = true;
    ui.post(clock);
    if (wanted)
      startStream();
  }
  @Override
  protected void onPause() {
    active = false;
    disconnect();
    ui.removeCallbacks(clock);
    super.onPause();
  }
  @Override
  protected void onDestroy() {
    posts.shutdownNow();
    super.onDestroy();
  }
  @Override
  protected void onSaveInstanceState(
      Bundle out) { /* Never save pending input or request contents. */ }

  private int dp(int n) {
    return (int) (n * getResources().getDisplayMetrics().density + 0.5f);
  }
  private LinearLayout.LayoutParams wide() {
    return new LinearLayout.LayoutParams(-1, -2);
  }
  private void margin(View v, int top, int bottom) {
    LinearLayout.LayoutParams p = wide();
    p.setMargins(0, dp(top), 0, dp(bottom));
    v.setLayoutParams(p);
  }
  private TextView add(LinearLayout parent, String value, int size, int color, boolean bold) {
    TextView t = new TextView(this);
    t.setText(value);
    t.setTextSize(size);
    t.setTextColor(color);
    t.setLineSpacing(dp(3), 1);
    if (bold)
      t.setTypeface(Typeface.DEFAULT, Typeface.BOLD);
    parent.addView(t, wide());
    return t;
  }
  private LinearLayout card(LinearLayout parent) {
    LinearLayout box = new LinearLayout(this);
    box.setOrientation(LinearLayout.VERTICAL);
    box.setPadding(dp(18), dp(18), dp(18), dp(18));
    GradientDrawable bg = new GradientDrawable();
    bg.setColor(CARD);
    bg.setCornerRadius(dp(18));
    box.setBackground(bg);
    parent.addView(box, wide());
    return box;
  }
  private Button button(LinearLayout parent, String text, boolean primary) {
    Button b = new Button(this);
    b.setText(text);
    b.setAllCaps(false);
    b.setTextSize(16);
    b.setMinHeight(dp(50));
    b.setTextColor(primary ? BG : FG);
    b.setBackgroundTintList(
        android.content.res.ColorStateList.valueOf(primary ? ACCENT : Color.rgb(49, 54, 72)));
    parent.addView(b, wide());
    margin(b, 10, 0);
    return b;
  }
  private void connectionState(String text, int color) {
    status.setText(text);
    status.setTextColor(color);
  }
  private void clearValues() {
    for (EditText f : fields) f.getText().clear();
  }
  private void resetContent() {
    clearValues();
    fields.clear();
    names.clear();
    expiry = null;
    approve = null;
    reject = null;
    currentId = "";
    submitting = false;
    content.removeAllViews();
  }
  private void showIdle(String title, String detail) {
    resetContent();
    LinearLayout box = card(content);
    add(box, title, 22, FG, true);
    TextView d = add(box, detail, 15, MUTED, false);
    margin(d, 10, 0);
    if (wanted) {
      Button demo = button(box, "Try a demo request", true);
      demo.setOnClickListener(v -> {
        demo.setEnabled(false);
        post("/v1/demo", new JSONObject(), null, () -> demo.setEnabled(true));
      });
    }
  }
  private void disconnect() {
    generation.incrementAndGet();
    HttpURLConnection c = stream;
    if (c != null)
      c.disconnect();
    stream = null;
    clearValues();
    if (content != null) {
      showIdle(
          "Connection paused", "Reconnect with the app open. Entered values have been cleared.");
    }
  }
  private static String validateAddress(String text) throws Exception {
    URI uri = new URI(text.trim());
    String host = uri.getHost();
    if (!"http".equals(uri.getScheme()) || host == null || uri.getUserInfo() != null
        || uri.getQuery() != null || uri.getFragment() != null
        || !(uri.getPath() == null || uri.getPath().isEmpty() || uri.getPath().equals("/"))
        || uri.getPort() < 1 || uri.getPort() > 65535)
      throw new IOException();
    String[] parts = host.split("\\.");
    boolean tailnet = false;
    if (parts.length == 4) {
      int a = Integer.parseInt(parts[0]), b = Integer.parseInt(parts[1]);
      tailnet = a == 100 && b >= 64 && b <= 127;
      for (String p : parts) {
        int n = Integer.parseInt(p);
        if (n < 0 || n > 255 || !Integer.toString(n).equals(p))
          throw new IOException();
      }
    }
    if (!tailnet && !(BuildConfig.DEBUG && host.equals("10.0.2.2")))
      throw new IOException();
    return "http://" + host + ":" + uri.getPort();
  }
  private void startStream() {
    int token = generation.incrementAndGet();
    HttpURLConnection old = stream;
    if (old != null)
      old.disconnect();
    clearValues();
    currentId = "";
    connectionState("Connecting…", MUTED);
    new Thread(() -> {
      int retry = 1;
      while (active && wanted && generation.get() == token) {
        HttpURLConnection c = null;
        try {
          c = (HttpURLConnection) URI.create(base + "/v1/events").toURL().openConnection();
          stream = c;
          c.setConnectTimeout(7000);
          c.setReadTimeout(25000);
          c.setInstanceFollowRedirects(false);
          c.setRequestProperty("Accept", "text/event-stream");
          int code = c.getResponseCode();
          if (code != 200)
            throw new IOException(code == 403 ? "This phone is not allowed by the bridge"
                                              : "Bridge returned HTTP " + code);
          if (!c.getContentType().startsWith("text/event-stream"))
            throw new IOException("Unexpected server response");
          retry = 1;
          try (BufferedReader reader = new BufferedReader(
                   new InputStreamReader(c.getInputStream(), StandardCharsets.UTF_8))) {
            StringBuilder line = new StringBuilder();
            String data = null;
            int ch;
            while (active && wanted && generation.get() == token && (ch = reader.read()) != -1) {
              if (ch == '\n') {
                String s = line.toString();
                line.setLength(0);
                if (s.isEmpty() && data != null) {
                  JSONObject snapshot = new JSONObject(data);
                  data = null;
                  ui.post(() -> {
                    if (active && wanted && generation.get() == token)
                      render(snapshot);
                  });
                } else if (s.startsWith("data: "))
                  data = s.substring(6);
              } else if (ch != '\r') {
                if (line.length() >= 1048576)
                  throw new IOException("Request is too large");
                line.append((char) ch);
              }
            }
          }
          throw new IOException("Connection interrupted");
        } catch (Exception e) {
          final String message = e instanceof IOException && e.getMessage() != null
              ? e.getMessage()
              : "Unable to reach the bridge";
          ui.post(() -> {
            if (active && wanted && generation.get() == token) {
              connectionState(message + " · reconnecting", MUTED);
              showIdle(
                  "Reconnecting…", "Check Tailscale and that your computer’s bridge is running.");
            }
          });
        } finally {
          if (c != null)
            c.disconnect();
        }
        if (generation.get() != token)
          break;
        try {
          Thread.sleep(retry * 1000L);
        } catch (InterruptedException e) {
          break;
        }
        retry = Math.min(retry * 2, 8);
      }
    }, "omaquery-events").start();
  }
  private void render(JSONObject snapshot) {
    try {
      hostname = snapshot.getString("hostname");
      connectionState("Connected to " + hostname, GREEN);
      JSONArray requests = snapshot.getJSONArray("requests");
      if (requests.length() == 0) {
        String message = snapshot.optString("demo_status", "");
        showIdle(message.isEmpty() ? "Waiting for a request" : message,
            message.isEmpty()
                ? "Run a command with omaquery run --mobile on your computer, or try a demo."
                : "The demo uses a dummy token. Your value is never displayed or saved.");
        return;
      }
      JSONObject envelope = requests.getJSONObject(0), req = envelope.getJSONObject("request");
      String id = req.getString("id");
      long deadline = Instant.parse(envelope.getString("expires_at")).toEpochMilli();
      if (deadline <= System.currentTimeMillis()) {
        showIdle("Request expired", "Waiting for an up-to-date request.");
        return;
      }
      if (id.equals(currentId))
        return;
      resetContent();
      currentId = id;
      expires = deadline;
      LinearLayout box = card(content);
      add(box, "COMMAND APPROVAL", 12, ACCENT, true);
      TextView from = add(box, "Requested on " + hostname, 20, FG, true);
      margin(from, 10, 8);
      add(box, "WORKING DIRECTORY", 11, MUTED, true);
      TextView cwd = add(box, req.getString("cwd"), 14, FG, false);
      cwd.setTypeface(Typeface.MONOSPACE);
      margin(cwd, 4, 16);
      add(box, "EXACT COMMAND · JSON ARGUMENTS", 11, MUTED, true);
      TextView command = add(box, req.getJSONArray("command").toString(2), 15, FG, false);
      command.setTypeface(Typeface.MONOSPACE);
      command.setTextIsSelectable(true);
      margin(command, 5, 16);
      String reason = req.optString("reason", "");
      if (!reason.isEmpty()) {
        add(box, "REASON", 11, MUTED, true);
        TextView r = add(box, reason, 15, FG, false);
        margin(r, 5, 16);
      }
      JSONArray specs = req.getJSONArray("fields");
      for (int i = 0; i < specs.length(); i++) {
        JSONObject spec = specs.getJSONObject(i);
        String name = spec.getString("name");
        names.add(name);
        add(box, name, 13, ACCENT, true);
        String hint = spec.optString("hint", "");
        if (!hint.isEmpty())
          add(box, hint, 13, MUTED, false);
        EditText input = new EditText(this);
        input.setTextColor(FG);
        input.setHintTextColor(MUTED);
        input.setTextSize(18);
        input.setHint("Enter value");
        input.setSingleLine(true);
        input.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_PASSWORD
            | InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS);
        input.setSaveEnabled(false);
        input.setImportantForAutofill(View.IMPORTANT_FOR_AUTOFILL_NO);
        input.setImeOptions(android.view.inputmethod.EditorInfo.IME_FLAG_NO_PERSONALIZED_LEARNING);
        box.addView(input, wide());
        margin(input, 4, 16);
        fields.add(input);
      }
      TextView warning = add(box,
          "Values go to this command’s environment. The command can print or transmit them. Only "
          + "approve commands you trust.",
          13, MUTED, false);
      margin(warning, 0, 12);
      expiry = add(box, "", 13, MUTED, false);
      approve = button(box, "Approve and run", true);
      reject = button(box, "Reject request", false);
      approve.setOnClickListener(v -> respond(false));
      reject.setOnClickListener(v -> respond(true));
      if (requests.length() > 1) {
        TextView queued =
            add(content, (requests.length() - 1) + " more request(s) waiting", 13, MUTED, false);
        margin(queued, 12, 0);
      }
    } catch (Exception e) {
      connectionState("Invalid request from bridge", MUTED);
      showIdle("Cannot display this request", "Reconnect and try again.");
    }
  }
  private void respond(boolean cancelled) {
    if (submitting || currentId.isEmpty() || expires <= System.currentTimeMillis())
      return;
    try {
      JSONObject payload = new JSONObject();
      payload.put("id", currentId);
      payload.put("cancelled", cancelled);
      if (!cancelled) {
        JSONObject values = new JSONObject();
        for (int i = 0; i < fields.size(); i++) {
          String value = fields.get(i).getText().toString();
          if (value.isEmpty()) {
            fields.get(i).setError("Enter a value");
            fields.get(i).requestFocus();
            return;
          }
          values.put(names.get(i), value);
        }
        payload.put("values", values);
      }
      submitting = true;
      approve.setEnabled(false);
      reject.setEnabled(false);
      approve.setText(R.string.sending);
      ((InputMethodManager) getSystemService(INPUT_METHOD_SERVICE))
          .hideSoftInputFromWindow(root.getWindowToken(), 0);
      clearValues();
      String id = currentId;
      post("/v1/response", payload, id, () -> {
        if (id.equals(currentId)) {
          submitting = false;
          if (approve != null) {
            approve.setText(R.string.approve);
            approve.setEnabled(true);
          }
          if (reject != null)
            reject.setEnabled(true);
        }
      });
    } catch (Exception e) {
      connectionState("Could not prepare response", MUTED);
    }
  }
  private void post(String path, JSONObject payload, String requestId, Runnable failure) {
    int token = generation.get();
    String target = base;
    posts.execute(() -> {
      HttpURLConnection c = null;
      try {
        byte[] bytes = payload.toString().getBytes(StandardCharsets.UTF_8);
        c = (HttpURLConnection) URI.create(target + path).toURL().openConnection();
        c.setConnectTimeout(7000);
        c.setReadTimeout(7000);
        c.setInstanceFollowRedirects(false);
        c.setRequestMethod("POST");
        c.setDoOutput(true);
        c.setRequestProperty("Content-Type", "application/json");
        c.setFixedLengthStreamingMode(bytes.length);
        try (OutputStream out = c.getOutputStream()) {
          out.write(bytes);
        }
        java.util.Arrays.fill(bytes, (byte) 0);
        int code = c.getResponseCode();
        if (code != 204 && code != 202)
          throw new IOException(code == 410 ? "Request expired or already answered"
                                            : "Response was not accepted (HTTP " + code + ")");
        ui.post(() -> {
          if (active && generation.get() == token && requestId != null
              && requestId.equals(currentId))
            showIdle("Response sent", "Waiting for your computer to finish the command.");
        });
      } catch (Exception e) {
        ui.post(() -> {
          if (active && generation.get() == token) {
            connectionState("Could not send response. Re-enter values to retry.", MUTED);
            failure.run();
          }
        });
      } finally {
        if (c != null)
          c.disconnect();
      }
    });
  }
}
