package com.omaquery.companion;

import android.Manifest;
import android.app.Activity;
import android.app.NotificationManager;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.pm.PackageManager;
import android.graphics.Color;
import android.graphics.drawable.GradientDrawable;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.widget.Button;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;
import org.json.JSONArray;
import org.json.JSONObject;

public final class MainActivity extends Activity {
  private final Handler ui = new Handler(Looper.getMainLooper());
  private EditText address;
  private TextView status;
  private Button toggle;
  private LinearLayout pending;
  private SharedPreferences prefs;
  private String rendered = "";
  private final Runnable refresh = new Runnable() {
    public void run() {
      render();
      ui.postDelayed(this, 1000);
    }
  };
  private int dp(int n) {
    return (int) (n * getResources().getDisplayMetrics().density + .5f);
  }
  private TextView text(LinearLayout parent, String value, int size) {
    TextView view = new TextView(this);
    view.setText(value);
    view.setTextSize(size);
    view.setTextColor(Color.rgb(235, 236, 248));
    view.setPadding(0, dp(10), 0, dp(10));
    parent.addView(view, new LinearLayout.LayoutParams(-1, -2));
    return view;
  }
  @Override
  public void onCreate(Bundle state) {
    super.onCreate(state);
    prefs = getSharedPreferences("connection", MODE_PRIVATE);
    ScrollView scroll = new ScrollView(this);
    scroll.setBackgroundColor(Color.rgb(18, 20, 28));
    LinearLayout root = new LinearLayout(this);
    root.setOrientation(LinearLayout.VERTICAL);
    root.setPadding(dp(22), dp(24), dp(22), dp(24));
    scroll.addView(root);
    setContentView(scroll);
    if (Build.VERSION.SDK_INT >= 30)
      scroll.setOnApplyWindowInsetsListener((v, insets) -> {
        android.graphics.Insets i = insets.getInsets(
            android.view.WindowInsets.Type.systemBars() | android.view.WindowInsets.Type.ime());
        root.setPadding(dp(22) + i.left, dp(24) + i.top, dp(22) + i.right, dp(24) + i.bottom);
        return insets;
      });
    else
      scroll.setFitsSystemWindows(true);
    text(root, "omaquery", 28);
    LinearLayout box = new LinearLayout(this);
    box.setOrientation(LinearLayout.VERTICAL);
    box.setPadding(dp(18), dp(18), dp(18), dp(18));
    GradientDrawable bg = new GradientDrawable();
    bg.setColor(Color.rgb(29, 32, 45));
    bg.setCornerRadius(dp(18));
    box.setBackground(bg);
    root.addView(box, new LinearLayout.LayoutParams(-1, -2));
    text(box, "Computer", 14);
    address = new EditText(this);
    address.setTextColor(Color.WHITE);
    address.setTextSize(17);
    address.setSingleLine(true);
    address.setSaveEnabled(false);
    address.setInputType(
        android.text.InputType.TYPE_CLASS_TEXT | android.text.InputType.TYPE_TEXT_VARIATION_URI);
    address.setHint("IP or hostname:port");
    address.setHintTextColor(Color.rgb(168, 174, 196));
    address.setText(
        BridgeClient.displayAddress(prefs.getString("address", BuildConfig.DEFAULT_BRIDGE)));
    address.setImeOptions(android.view.inputmethod.EditorInfo.IME_ACTION_DONE);
    box.addView(address, new LinearLayout.LayoutParams(-1, -2));
    toggle = new Button(this);
    toggle.setAllCaps(false);
    box.addView(toggle, new LinearLayout.LayoutParams(-1, -2));
    toggle.setOnClickListener(v -> {
      if (prefs.getBoolean("enabled", false)) {
        prefs.edit().putBoolean("enabled", false).apply();
        stopService(new Intent(this, BridgeService.class));
        render();
      } else if (Build.VERSION.SDK_INT >= 33
          && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS)
              != PackageManager.PERMISSION_GRANTED) {
        requestPermissions(new String[] {Manifest.permission.POST_NOTIFICATIONS}, 10);
      } else
        enable();
    });
    address.setOnEditorActionListener((v, action, event) -> {
      if (action == android.view.inputmethod.EditorInfo.IME_ACTION_DONE) {
        if (!prefs.getBoolean("enabled", false))
          toggle.performClick();
        return true;
      }
      return false;
    });
    status = text(box, "", 14);
    text(root, "Tailscale must be connected.", 13).setTextColor(Color.rgb(168, 174, 196));
    pending = new LinearLayout(this);
    pending.setOrientation(LinearLayout.VERTICAL);
    root.addView(pending, new LinearLayout.LayoutParams(-1, -2));
    render();
  }
  private void enable() {
    try {
      if (!getSystemService(NotificationManager.class).areNotificationsEnabled()) {
        status.setText(R.string.allow_notifications);
        return;
      }
      String base = BridgeClient.validateAddress(address.getText().toString());
      prefs.edit().putString("address", base).putBoolean("enabled", true).apply();
      startForegroundService(new Intent(this, BridgeService.class));
      render();
    } catch (Exception e) {
      address.setError("Enter IP:port or hostname:port");
    }
  }
  @Override
  public void onRequestPermissionsResult(int code, String[] permissions, int[] results) {
    super.onRequestPermissionsResult(code, permissions, results);
    if (code == 10 && results.length > 0 && results[0] == PackageManager.PERMISSION_GRANTED)
      enable();
    else
      status.setText(R.string.notifications_needed);
  }
  @Override
  protected void onResume() {
    super.onResume();
    if (prefs.getBoolean("enabled", false))
      startForegroundService(new Intent(this, BridgeService.class));
    ui.post(refresh);
  }
  @Override
  protected void onPause() {
    ui.removeCallbacks(refresh);
    super.onPause();
  }
  private void render() {
    boolean enabled = prefs.getBoolean("enabled", false);
    address.setEnabled(!enabled);
    toggle.setText(enabled ? "Disconnect" : "Connect");
    status.setText(BridgeService.status());
    JSONObject snapshot = BridgeService.snapshot();
    String next = snapshot == null ? "" : snapshot.toString();
    if (next.equals(rendered))
      return;
    rendered = next;
    pending.removeAllViews();
    if (snapshot == null)
      return;
    try {
      JSONArray requests = snapshot.getJSONArray("requests");
      if (requests.length() == 0) {
        return;
      }
      text(pending, "Pending approvals", 20);
      for (int i = 0; i < requests.length(); i++) {
        JSONObject item = requests.getJSONObject(i);
        if (java.time.Instant.parse(item.getString("expires_at")).toEpochMilli()
            <= System.currentTimeMillis())
          continue;
        String id = item.getJSONObject("request").getString("id");
        Button open = new Button(this);
        open.setAllCaps(false);
        open.setText(getString(R.string.review_request, snapshot.getString("hostname")));
        pending.addView(open, new LinearLayout.LayoutParams(-1, -2));
        open.setOnClickListener(v
            -> startActivity(new Intent(this, ApprovalActivity.class).putExtra("request_id", id)));
      }
    } catch (Exception e) {
      status.setText(R.string.pending_error);
    }
  }
}
