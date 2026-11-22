#!/system/bin/sh
# One-shot debloat: remount /system rw, remove vendor APKs, remount ro.
mount -o remount,rw /system && echo "[ok] /system rw" || echo "[!!] remount rw failed"

for d in app/FotaUpdate app/RKUpdateService app/DeviceTest app/RkExplorer \
         app/RkVideoPlayer app/MediaFloat app/RkApkinstaller \
         priv-app/StressTest priv-app/InputDevices; do
  if [ -d "/system/$d" ]; then
    rm -rf "/system/$d" && echo "[ok] removed $d" || echo "[!!] FAILED $d"
  else
    echo "[--] already gone: $d"
  fi
done

sync
mount -o remount,ro /system && echo "[ok] /system ro" || echo "[!!] remount ro failed"
echo "=== remaining vendor dirs ==="
ls /system/app | grep -iE "fota|update|device|rk|mediafloat|explorer|video|installer" || echo "(none in app)"
ls /system/priv-app | grep -iE "stress|input|frameo" || echo "(none in priv-app)"
