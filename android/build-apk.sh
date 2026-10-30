#!/usr/bin/env bash
# Native Java/Android SDK build, no Gradle downloads or third-party libraries.
set -euo pipefail
cd "$(dirname "$0")"
: "${ANDROID_HOME:?Set ANDROID_HOME to your Android SDK directory}"
: "${JAVA_HOME:?Set JAVA_HOME to JDK 21 or newer}"
SDK_PLATFORM="${SDK_PLATFORM:-36}"
BUILD_TOOLS="${BUILD_TOOLS:-36.0.0}"
tools="$ANDROID_HOME/build-tools/$BUILD_TOOLS"
platform="$ANDROID_HOME/platforms/android-$SDK_PLATFORM/android.jar"
out="$PWD/build"
mkdir -p "$out/classes" "$out/generated/com/omaquery/companion" "$out/dex"
# Only a public bridge URL can be embedded. Never embed tokens or credentials.
bridge="${OMAQUERY_BRIDGE_URL:-}"
if [[ -n "$bridge" && ! "$bridge" =~ ^http://100\.[0-9]+\.[0-9]+\.[0-9]+:[0-9]+$ ]]; then
  echo 'OMAQUERY_BRIDGE_URL must be a literal tailnet IPv4 HTTP URL' >&2; exit 1
fi
cat > "$out/generated/com/omaquery/companion/BuildConfig.java" <<CONFIG
package com.omaquery.companion;
public final class BuildConfig {
  public static final boolean DEBUG = true;
  public static final String DEFAULT_BRIDGE = "$bridge";
}
CONFIG
"$tools/aapt2" compile --dir app/src/main/res -o "$out/resources.zip"
"$tools/aapt2" link -o "$out/unsigned.apk" -I "$platform" --manifest app/src/main/AndroidManifest.xml --java "$out/generated" --version-code 2 --version-name 0.2.0-notifications "$out/resources.zip"
mapfile -t java_sources < <(find app/src/main/java "$out/generated" -name '*.java' -print)
"$JAVA_HOME/bin/javac" -Xlint:all -encoding UTF-8 -source 17 -target 17 -classpath "$platform" -d "$out/classes" "${java_sources[@]}"
"$JAVA_HOME/bin/jar" --create --file "$out/classes.jar" -C "$out/classes" .
JAVA_HOME="$JAVA_HOME" "$tools/d8" --lib "$platform" --min-api 26 --output "$out/dex" "$out/classes.jar"
(cd "$out/dex" && zip -q "$out/unsigned.apk" classes.dex)
"$tools/zipalign" -f -p 4 "$out/unsigned.apk" "$out/aligned.apk"
# Reuse Android's existing development key. This is a sideload-only preview.
keystore="${ANDROID_DEBUG_KEYSTORE:-$HOME/.android/debug.keystore}"
if [[ ! -f "$keystore" ]]; then
  echo 'An Android debug keystore is required; set ANDROID_DEBUG_KEYSTORE.' >&2; exit 1
fi
JAVA_HOME="$JAVA_HOME" "$tools/apksigner" sign --ks "$keystore" --ks-key-alias androiddebugkey --ks-pass pass:android --key-pass pass:android --out "$out/omaquery-companion.apk" "$out/aligned.apk"
JAVA_HOME="$JAVA_HOME" "$tools/apksigner" verify --verbose "$out/omaquery-companion.apk"
printf '\nAPK: %s\n' "$out/omaquery-companion.apk"
