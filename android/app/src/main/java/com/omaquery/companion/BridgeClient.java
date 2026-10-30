package com.omaquery.companion;

import java.io.IOException;
import java.net.URI;

final class BridgeClient {
  static String validateAddress(String text) throws Exception {
    URI uri = new URI(text.trim());
    String host = uri.getHost();
    if (!"http".equals(uri.getScheme()) || host == null || uri.getUserInfo() != null
        || uri.getQuery() != null || uri.getFragment() != null
        || !(uri.getPath() == null || uri.getPath().isEmpty() || uri.getPath().equals("/"))
        || uri.getPort() < 1 || uri.getPort() > 65535)
      throw new IOException("Use a Tailscale IP and port");
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
}
