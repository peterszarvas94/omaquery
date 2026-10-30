package com.omaquery.companion;

import android.app.Activity;
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
import java.io.IOException;
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

/** Notification-launched approval screen. Values stay in this activity and its POST. */
public final class ApprovalActivity extends Activity {
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
  private Button approve, reject;
  private volatile String base = "";
  private String currentId = "", hostname = "", requestedId = "";
  private long expires;
  private boolean active = false, submitting = false;
  private String outcome = "";
  private final Runnable clock = new Runnable() {
    public void run() {
      JSONObject latest = BridgeService.snapshot();
      if (latest != null)
        render(latest);
      else {
        resetContent();
        connectionState("Reconnecting — open connections to check the listener", MUTED);
      }
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
    requestedId = getIntent().getStringExtra("request_id");
    if (requestedId == null)
      requestedId = "";
    TextView back = add(root, "‹ Connections", 15, ACCENT, false);
    back.setPadding(0, dp(10), 0, dp(14));
    back.setMinHeight(dp(48));
    back.setFocusable(true);
    back.setOnClickListener(v -> {
      startActivity(new android.content.Intent(this, MainActivity.class)
              .addFlags(android.content.Intent.FLAG_ACTIVITY_CLEAR_TOP));
      finish();
    });
    status = add(root, "Loading request…", 14, MUTED, false);
    content = new LinearLayout(this);
    content.setOrientation(LinearLayout.VERTICAL);
    root.addView(content, wide());
    showIdle("Loading approval", "Fetching the current request from your computer.");
  }
  @Override
  protected void onResume() {
    super.onResume();
    if (BridgeService.instance == null
        && getSharedPreferences("connection", MODE_PRIVATE).getBoolean("enabled", false))
      startForegroundService(new android.content.Intent(this, BridgeService.class));
    active = true;
    ui.post(clock);
    base = BridgeService.address();
    if (!outcome.isEmpty())
      showIdle(outcome, "You can return to connections.");
  }
  @Override
  protected void onPause() {
    active = false;
    generation.incrementAndGet();
    resetContent();
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
    bg.setCornerRadius(dp(10));
    bg.setStroke(dp(1), Color.rgb(63, 68, 88));
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
  }
  private void render(JSONObject snapshot) {
    if (!outcome.isEmpty())
      return;
    try {
      hostname = snapshot.getString("hostname");
      connectionState("Connected to " + hostname, GREEN);
      JSONArray requests = snapshot.getJSONArray("requests");
      if (requests.length() == 0) {
        showIdle("Request no longer pending",
            "It was answered, cancelled or expired. You can return to connections.");
        return;
      }
      JSONObject envelope = null;
      for (int i = 0; i < requests.length(); i++) {
        JSONObject candidate = requests.getJSONObject(i);
        if (candidate.getJSONObject("request").getString("id").equals(requestedId)) {
          envelope = candidate;
          break;
        }
      }
      if (envelope == null) {
        showIdle("Request no longer pending",
            "It was answered, cancelled or expired. You can return to connections.");
        return;
      }
      JSONObject req = envelope.getJSONObject("request");
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
      // Pin approval POSTs to the address on which this request was reviewed.
      base = BridgeService.address();
      expires = deadline;
      LinearLayout box = card(content);
      LinearLayout hero = new LinearLayout(this);
      hero.setGravity(android.view.Gravity.CENTER_VERTICAL);
      box.addView(hero, wide());
      android.widget.ImageView icon = new android.widget.ImageView(this);
      icon.setImageResource(R.drawable.ic_omaquery);
      hero.addView(icon, new LinearLayout.LayoutParams(dp(36), dp(36)));
      LinearLayout titles = new LinearLayout(this);
      titles.setOrientation(LinearLayout.VERTICAL);
      titles.setPadding(dp(12), 0, 0, 0);
      hero.addView(titles, new LinearLayout.LayoutParams(0, -2, 1));
      add(titles, "omaquery", 24, FG, true);
      add(titles, "Command environment values", 12, MUTED, false);
      add(hero, "Remote", 12, ACCENT, false).setLayoutParams(new LinearLayout.LayoutParams(-2, -2));
      View separator = new View(this);
      separator.setBackgroundColor(Color.rgb(65, 69, 88));
      LinearLayout.LayoutParams rule = new LinearLayout.LayoutParams(-1, dp(1));
      rule.setMargins(0, dp(18), 0, dp(16));
      box.addView(separator, rule);
      add(box, "A command is requesting values for its environment.", 14, MUTED, false);
      TextView from = add(box, "Remote request from " + hostname, 14, ACCENT, false);
      margin(from, 8, 16);
      LinearLayout details = card(box);
      add(details, "WORKING DIRECTORY", 11, MUTED, true);
      TextView cwd = add(details, req.getString("cwd"), 14, MUTED, false);
      cwd.setTypeface(Typeface.MONOSPACE);
      margin(cwd, 4, 16);
      add(details, "COMMAND", 11, MUTED, true);
      JSONArray arguments = req.getJSONArray("command");
      StringBuilder commandLine = new StringBuilder();
      for (int i = 0; i < arguments.length(); i++) {
        if (i > 0)
          commandLine.append(' ');
        commandLine.append(JSONObject.quote(arguments.getString(i)));
      }
      TextView command = add(details, commandLine.toString(), 15, FG, false);
      command.setTypeface(Typeface.MONOSPACE);
      command.setTextIsSelectable(true);
      margin(command, 5, 0);
      margin(details, 0, 18);
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
        input.setHint("Enter or paste value");
        input.setSingleLine(true);
        input.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_PASSWORD
            | InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS);
        input.setSaveEnabled(false);
        input.setImportantForAutofill(View.IMPORTANT_FOR_AUTOFILL_NO);
        input.setImeOptions(android.view.inputmethod.EditorInfo.IME_FLAG_NO_PERSONALIZED_LEARNING);
        LinearLayout entry = new LinearLayout(this);
        entry.setGravity(android.view.Gravity.CENTER_VERTICAL);
        box.addView(entry, wide());
        entry.addView(input, new LinearLayout.LayoutParams(0, -2, 1));
        Button reveal = new Button(this);
        reveal.setAllCaps(false);
        reveal.setText(R.string.show);
        reveal.setTextColor(ACCENT);
        reveal.setMinWidth(dp(64));
        entry.addView(reveal, new LinearLayout.LayoutParams(-2, -2));
        reveal.setOnClickListener(v -> {
          boolean hidden = input.getTransformationMethod()
                               instanceof android.text.method.PasswordTransformationMethod;
          input.setTransformationMethod(
              hidden ? null : android.text.method.PasswordTransformationMethod.getInstance());
          reveal.setText(hidden ? R.string.hide : R.string.show);
          input.setSelection(input.length());
        });
        margin(entry, 4, 16);
        fields.add(input);
      }
      TextView warning =
          add(box, "Values go to this command. Its output may expose them.", 13, MUTED, false);
      margin(warning, 0, 12);
      expiry = add(box, "", 13, MUTED, false);
      LinearLayout actions = new LinearLayout(this);
      box.addView(actions, wide());
      reject = button(actions, "Cancel", false);
      approve = button(actions, "Run command", true);
      LinearLayout.LayoutParams cancelLayout = new LinearLayout.LayoutParams(0, -2, 1);
      cancelLayout.setMargins(0, 0, dp(8), 0);
      reject.setLayoutParams(cancelLayout);
      approve.setLayoutParams(new LinearLayout.LayoutParams(0, -2, 1));
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
              && requestId.equals(requestedId)) {
            outcome = payload.optBoolean("cancelled") ? "Request rejected" : "Approved and sent";
            showIdle(outcome, "You can return to connections.");
          }
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
