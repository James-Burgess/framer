#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
SDK_DIR="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-$HOME/Library/Android/sdk}}"
BUILD_TOOLS="$SDK_DIR/build-tools/35.0.0"
ANDROID_JAR="$SDK_DIR/platforms/android-34/android.jar"

if [[ ! -d "$BUILD_TOOLS" ]]; then
  BUILD_TOOLS="$(ls -d "$SDK_DIR/build-tools/"* | tail -n 1)"
fi
if [[ ! -f "$ANDROID_JAR" ]]; then
  ANDROID_JAR="$(ls "$SDK_DIR/platforms/"android-*/android.jar | tail -n 1)"
fi

OUT_DIR="$ROOT_DIR/dist"
WORK_DIR="$OUT_DIR/build_tmp"
rm -rf "$WORK_DIR"
mkdir -p "$WORK_DIR/gen" "$WORK_DIR/classes" "$WORK_DIR/dex" "$WORK_DIR/apk/lib/armeabi-v7a" "$OUT_DIR"

echo "==> 1. Cross-compiling Go server for Android ARMv7 (armeabi-v7a)..."
(
  cd "$ROOT_DIR/server"
  GOTOOLCHAIN=local GOCACHE="${GOCACHE:-/tmp/gocache}" \
    GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 \
    go build -ldflags="-s -w" -o "$WORK_DIR/apk/lib/armeabi-v7a/libfrmaeo.so" .
)

echo "==> 2. Compiling Android resources with aapt2..."
"$BUILD_TOOLS/aapt2" compile --dir "$ROOT_DIR/android/res" -o "$WORK_DIR/res.zip"
"$BUILD_TOOLS/aapt2" link \
  -I "$ANDROID_JAR" \
  --manifest "$ROOT_DIR/android/AndroidManifest.xml" \
  --java "$WORK_DIR/gen" \
  -o "$WORK_DIR/unsigned.apk" \
  "$WORK_DIR/res.zip"

echo "==> 3. Compiling Java sources (Java 8 bytecode for Android 6.0.1)..."
javac --release 8 \
  -classpath "$ANDROID_JAR" \
  -d "$WORK_DIR/classes" \
  "$WORK_DIR/gen/com/frmaeo/kiosk/R.java" \
  "$ROOT_DIR/android/src/com/frmaeo/kiosk/"*.java

echo "==> 4. Converting classes to DEX with d8..."
"$BUILD_TOOLS/d8" \
  --min-api 23 \
  --lib "$ANDROID_JAR" \
  --output "$WORK_DIR/dex" \
  "$WORK_DIR/classes/com/frmaeo/kiosk/"*.class

echo "==> 5. Packaging classes.dex + libfrmaeo.so into APK..."
cp "$WORK_DIR/dex/classes.dex" "$WORK_DIR/apk/classes.dex"
(
  cd "$WORK_DIR/apk"
  zip -q -u "$WORK_DIR/unsigned.apk" classes.dex
  # Store native binary uncompressed for fast extraction & proper alignment
  zip -q -0 -u "$WORK_DIR/unsigned.apk" lib/armeabi-v7a/libfrmaeo.so
)

echo "==> 6. Zipaligning & signing APK..."
KEYSTORE="$ROOT_DIR/android/debug.keystore"
if [[ ! -f "$KEYSTORE" ]]; then
  keytool -genkeypair -v \
    -keystore "$KEYSTORE" \
    -storepass android -keypass android \
    -alias androiddebugkey \
    -keyalg RSA -keysize 2048 -validity 10000 \
    -dname "CN=frmaeo,O=Local,C=US" >/dev/null 2>&1
fi

"$BUILD_TOOLS/zipalign" -f -p 4 "$WORK_DIR/unsigned.apk" "$WORK_DIR/aligned.apk"
"$BUILD_TOOLS/apksigner" sign \
  --ks "$KEYSTORE" \
  --ks-pass pass:android \
  --key-pass pass:android \
  --out "$OUT_DIR/frmaeo-kiosk.apk" \
  "$WORK_DIR/aligned.apk"

"$BUILD_TOOLS/apksigner" verify "$OUT_DIR/frmaeo-kiosk.apk"
rm -rf "$WORK_DIR"
ls -lh "$OUT_DIR/frmaeo-kiosk.apk"
echo "==> Done: $OUT_DIR/frmaeo-kiosk.apk"
