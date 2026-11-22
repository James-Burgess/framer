#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
"$ROOT_DIR/scripts/build_apk.sh"

echo "==> Checking ADB device..."
adb devices -l

echo "==> Installing frmaeo-kiosk.apk..."
adb install -r "$ROOT_DIR/dist/frmaeo-kiosk.apk"

echo "==> Setting device hostname to 'frame' (requires root)..."
adb shell "su 0 setprop net.hostname frame" || true

echo "==> Launching frmaeo Kiosk..."
adb shell am start -n com.frmaeo.kiosk/.MainActivity

echo "==> Deployed! Tip: set frmaeo as the default Home app, then you can safely disable net.frameo.frame."
