package com.omaquery.companion;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.app.TaskStackBuilder;
import android.content.Intent;
import android.content.pm.ServiceInfo;
import android.net.Uri;
import android.os.Build;
import android.os.Handler;
import android.os.IBinder;
import android.os.Looper;
import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStreamReader;
import java.net.HttpURLConnection;
import java.net.URI;
import java.nio.charset.StandardCharsets;
import java.time.Instant;
import java.util.HashMap;
import java.util.Iterator;
import java.util.Map;
import java.util.concurrent.atomic.AtomicInteger;
import org.json.JSONArray;
import org.json.JSONObject;

/** Owns the one background SSE connection. Never receives or stores secret values. */
public final class BridgeService extends Service {
  static volatile BridgeService instance;
  private static final String STATUS = "connection", REQUESTS = "approvals";
  private static final int STATUS_ID = 1, REQUEST_ID = 2;
  private final Handler main = new Handler(Looper.getMainLooper());
  private final AtomicInteger generation = new AtomicInteger();
  private final Map<String, Long> notified = new HashMap<>();
  private NotificationManager notifications;
  private volatile HttpURLConnection connection;
  private volatile JSONObject snapshot;
  private volatile String status = "Connecting…";
  private String base;
  private final Runnable expirySweep = new Runnable() {
    public void run() {
      Iterator<Map.Entry<String, Long>> it = notified.entrySet().iterator();
      while (it.hasNext()) {
        Map.Entry<String, Long> entry = it.next();
        if (entry.getValue() <= System.currentTimeMillis()) {
          notifications.cancel(entry.getKey(), REQUEST_ID);
          it.remove();
        }
      }
      main.postDelayed(this, 1000);
    }
  };
  static JSONObject snapshot() {
    BridgeService s = instance;
    return s == null ? null : s.snapshot;
  }
  static String status() {
    BridgeService s = instance;
    return s == null ? "Notifications disabled" : s.status;
  }
  static String address() {
    BridgeService s = instance;
    return s == null ? "" : s.base;
  }

  @Override
  public void onCreate() {
    super.onCreate();
    instance = this;
    notifications = getSystemService(NotificationManager.class);
    NotificationChannel channel =
        new NotificationChannel(STATUS, "Connection status", NotificationManager.IMPORTANCE_LOW);
    channel.setDescription("Shows when Omaquery is listening for requests");
    notifications.createNotificationChannel(channel);
    channel =
        new NotificationChannel(REQUESTS, "Command approvals", NotificationManager.IMPORTANCE_HIGH);
    channel.setDescription("Tap to review a command and enter its requested values");
    channel.setLockscreenVisibility(Notification.VISIBILITY_PRIVATE);
    notifications.createNotificationChannel(channel);
    main.post(expirySweep);
  }
  @Override
  public int onStartCommand(Intent intent, int flags, int startId) {
    if (intent != null && "stop".equals(intent.getAction())) {
      getSharedPreferences("connection", MODE_PRIVATE).edit().putBoolean("enabled", false).apply();
      stopSelf();
      return START_NOT_STICKY;
    }
    try {
      if (!getSharedPreferences("connection", MODE_PRIVATE).getBoolean("enabled", false)
          || !notifications.areNotificationsEnabled()) {
        stopSelf();
        return START_NOT_STICKY;
      }
      String address = BridgeClient.validateAddress(
          getSharedPreferences("connection", MODE_PRIVATE).getString("address", ""));
      if (Build.VERSION.SDK_INT >= 34)
        startForeground(
            STATUS_ID, statusNotification(), ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE);
      else
        startForeground(STATUS_ID, statusNotification());
      if (!address.equals(base)) {
        base = address;
        connect();
      }
      return START_STICKY;
    } catch (Exception e) {
      getSharedPreferences("connection", MODE_PRIVATE).edit().putBoolean("enabled", false).apply();
      stopSelf();
      return START_NOT_STICKY;
    }
  }
  private Notification statusNotification() {
    PendingIntent open = PendingIntent.getActivity(this, 0, new Intent(this, MainActivity.class),
        PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);
    PendingIntent stop =
        PendingIntent.getService(this, 1, new Intent(this, BridgeService.class).setAction("stop"),
            PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);
    return new Notification.Builder(this, STATUS)
        .setSmallIcon(R.drawable.ic_omaquery)
        .setContentTitle("Omaquery is listening")
        .setContentText(status)
        .setContentIntent(open)
        .setOngoing(true)
        .setOnlyAlertOnce(true)
        .addAction(new Notification.Action.Builder(null, "Stop listening", stop).build())
        .build();
  }
  private void clearRequests() {
    for (String id : notified.keySet()) notifications.cancel(id, REQUEST_ID);
    notified.clear();
    snapshot = null;
  }
  private void publish(JSONObject value) {
    try {
      JSONArray requests = value.getJSONArray("requests");
      Map<String, Long> pending = new HashMap<>();
      for (int i = 0; i < requests.length(); i++) {
        JSONObject envelope = requests.getJSONObject(i);
        String id = envelope.getJSONObject("request").getString("id");
        long expires = Instant.parse(envelope.getString("expires_at")).toEpochMilli();
        if (expires <= System.currentTimeMillis())
          continue;
        pending.put(id, expires);
        if (!notified.containsKey(id)) {
          Intent approval = new Intent(this, ApprovalActivity.class)
                                .setData(Uri.parse("omaquery://request/" + id))
                                .putExtra("request_id", id);
          PendingIntent tap =
              TaskStackBuilder.create(this).addNextIntentWithParentStack(approval).getPendingIntent(
                  0, PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE);
          Notification publicVersion = new Notification.Builder(this, REQUESTS)
                                           .setSmallIcon(R.drawable.ic_omaquery)
                                           .setContentTitle("Omaquery approval requested")
                                           .setContentText("Unlock to review the request")
                                           .build();
          notifications.notify(id, REQUEST_ID,
              new Notification.Builder(this, REQUESTS)
                  .setSmallIcon(R.drawable.ic_omaquery)
                  .setContentTitle("A command needs your approval")
                  .setContentText("Tap to review the command and enter its values")
                  .setContentIntent(tap)
                  .setAutoCancel(false)
                  .setOnlyAlertOnce(true)
                  .setVisibility(Notification.VISIBILITY_PRIVATE)
                  .setPublicVersion(publicVersion)
                  .setTimeoutAfter(expires - System.currentTimeMillis())
                  .build());
        }
      }
      for (String id : notified.keySet())
        if (!pending.containsKey(id))
          notifications.cancel(id, REQUEST_ID);
      notified.clear();
      notified.putAll(pending);
      snapshot = value;
      status = "Connected to " + value.getString("hostname");
      notifications.notify(STATUS_ID, statusNotification());
    } catch (Exception e) {
      clearRequests();
      status = "Invalid request from bridge";
      notifications.notify(STATUS_ID, statusNotification());
    }
  }
  private void connect() {
    final int token = generation.incrementAndGet();
    HttpURLConnection old = connection;
    if (old != null)
      old.disconnect();
    new Thread(() -> {
      int delay = 1;
      while (generation.get() == token) {
        HttpURLConnection c = null;
        try {
          c = (HttpURLConnection) URI.create(base + "/v1/events").toURL().openConnection();
          connection = c;
          c.setConnectTimeout(7000);
          c.setReadTimeout(25000);
          c.setInstanceFollowRedirects(false);
          c.setRequestProperty("Accept", "text/event-stream");
          if (c.getResponseCode() != 200)
            throw new IOException();
          if (c.getContentType() == null || !c.getContentType().startsWith("text/event-stream"))
            throw new IOException();
          delay = 1;
          try (BufferedReader reader = new BufferedReader(
                   new InputStreamReader(c.getInputStream(), StandardCharsets.UTF_8))) {
            StringBuilder line = new StringBuilder();
            String data = null;
            int ch;
            while (generation.get() == token && (ch = reader.read()) != -1) {
              if (ch == '\n') {
                String text = line.toString();
                line.setLength(0);
                if (text.isEmpty() && data != null) {
                  JSONObject value = new JSONObject(data);
                  data = null;
                  main.post(() -> {
                    if (generation.get() == token)
                      publish(value);
                  });
                } else if (text.startsWith("data: "))
                  data = text.substring(6);
              } else if (ch != '\r') {
                if (line.length() >= 1048576)
                  throw new IOException();
                line.append((char) ch);
              }
            }
          }
          throw new IOException("Event stream ended");
        } catch (Exception e) {
          main.post(() -> {
            if (generation.get() == token) {
              clearRequests();
              status = "Reconnecting — check Tailscale and your computer";
              notifications.notify(STATUS_ID, statusNotification());
            }
          });
        } finally {
          if (c != null)
            c.disconnect();
        }
        if (generation.get() != token)
          return;
        try {
          Thread.sleep(delay * 1000L);
        } catch (InterruptedException e) {
          return;
        }
        delay = Math.min(8, delay * 2);
      }
    }, "omaquery-background-events").start();
  }
  @Override
  public void onDestroy() {
    generation.incrementAndGet();
    HttpURLConnection c = connection;
    if (c != null)
      c.disconnect();
    main.removeCallbacksAndMessages(null);
    clearRequests();
    stopForeground(STOP_FOREGROUND_REMOVE);
    instance = null;
    super.onDestroy();
  }
  @Override
  public IBinder onBind(Intent intent) {
    return null;
  }
}
