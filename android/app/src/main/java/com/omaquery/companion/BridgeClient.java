package com.omaquery.companion;

import java.io.IOException;
import java.net.InetAddress;
import java.net.URI;
import java.util.Locale;

final class BridgeClient {
  // Syntax only: safe to call from the main thread. DNS runs in the service.
  static String validateAddress(String text) throws Exception {
    String value = text.trim();
    if (!value.contains("://"))
      value = "http://" + value;
    URI uri = new URI(value);
    String host = uri.getHost();
    if (!"http".equals(uri.getScheme()) || host == null || uri.getUserInfo() != null
        || uri.getQuery() != null || uri.getFragment() != null
        || !(uri.getPath() == null || uri.getPath().isEmpty() || uri.getPath().equals("/"))
        || uri.getPort() < 1 || uri.getPort() > 65535
        || !host.matches("(?i)[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?\\.?"))
      throw new IOException("Use IP:port or hostname:port");
    if (host.matches("[0-9.]+")) {
      String[] parts = host.split("\\.");
      if (parts.length != 4)
        throw new IOException();
      byte[] octets = new byte[4];
      for (int i = 0; i < 4; i++) {
        int n = Integer.parseInt(parts[i]);
        if (n < 0 || n > 255 || !Integer.toString(n).equals(parts[i]))
          throw new IOException();
        octets[i] = (byte) n;
      }
      if (!isTailnet(InetAddress.getByAddress(octets)))
        throw new IOException("Address is outside Tailscale");
    } else {
      for (String label : host.replaceFirst("\\.$", "").split("\\.", -1))
        if (label.isEmpty() || label.length() > 63 || label.startsWith("-") || label.endsWith("-"))
          throw new IOException();
    }
    return "http://" + host.toLowerCase(Locale.ROOT) + ":" + uri.getPort();
  }
  static boolean isTailnet(InetAddress address) {
    byte[] b = address.getAddress();
    if (b.length == 4) {
      if (BuildConfig.DEBUG && (b[0] & 255) == 10 && b[1] == 0 && b[2] == 2 && b[3] == 2)
        return true;
      return (b[0] & 255) == 100 && (b[1] & 255) >= 64 && (b[1] & 255) <= 127;
    }
    return b.length == 16 && (b[0] & 255) == 0xfd && (b[1] & 255) == 0x7a && (b[2] & 255) == 0x11
        && (b[3] & 255) == 0x5c && (b[4] & 255) == 0xa1 && (b[5] & 255) == 0xe0;
  }
  // Resolve through Android's DNS (including the Tailscale VPN's MagicDNS),
  // reject any non-tailnet answer, and pin HTTP/SSE/POST to a validated address.
  static String resolveAddress(android.content.Context context, String input) throws Exception {
    URI uri = new URI(validateAddress(input));
    java.util.List<String> candidates = new java.util.ArrayList<>();
    // Android's VPN search domain lets short machine names resolve within the
    // tailnet even if an underlying network has a conflicting local hostname.
    if (!uri.getHost().contains(".")) {
      android.net.ConnectivityManager manager =
          context.getSystemService(android.net.ConnectivityManager.class);
      android.net.Network network = manager.getActiveNetwork();
      android.net.LinkProperties properties =
          network == null ? null : manager.getLinkProperties(network);
      if (properties != null && properties.getDomains() != null) {
        for (String domain : properties.getDomains().split("[ ,]+"))
          if (!domain.isEmpty())
            candidates.add(uri.getHost() + "." + domain);
      }
    }
    candidates.add(uri.getHost());
    for (String candidate : candidates) {
      try {
        return resolved(uri, InetAddress.getAllByName(candidate));
      } catch (IOException ignored) { /* Try the next configured DNS search candidate. */
      }
    }
    throw new IOException("Host not found in Tailscale");
  }
  private static String resolved(URI uri, InetAddress[] addresses) throws Exception {
    if (addresses.length == 0)
      throw new IOException("Host not found in Tailscale");
    InetAddress chosen = addresses[0];
    for (InetAddress address : addresses) {
      if (!isTailnet(address))
        throw new IOException("Host resolves outside Tailscale");
      if (address.getAddress().length == 4)
        chosen = address;
    }
    return new URI("http", null, chosen.getHostAddress(), uri.getPort(), null, null, null)
        .toASCIIString();
  }
  static String displayAddress(String value) {
    return value.replaceFirst("^http://", "");
  }
}
