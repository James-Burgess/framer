package com.frmaeo.kiosk;

import android.app.Activity;
import android.content.Intent;
import android.os.Build;
import android.os.StatFs;
import android.provider.Settings;
import android.util.Log;
import android.view.WindowManager;
import android.webkit.JavascriptInterface;

import org.json.JSONObject;

import java.net.Inet4Address;
import java.net.InetAddress;
import java.net.NetworkInterface;
import java.util.Collections;

public class FrameBridge {
    private static final String TAG = "frmaeo-bridge";
    private final Activity activity;
    private int lastBrightness = 200;

    public FrameBridge(Activity activity) {
        this.activity = activity;
    }

    @JavascriptInterface
    public boolean isNative() {
        return true;
    }

    @JavascriptInterface
    public int getBrightness() {
        try {
            return Settings.System.getInt(activity.getContentResolver(), Settings.System.SCREEN_BRIGHTNESS);
        } catch (Exception e) {
            return lastBrightness;
        }
    }

    @JavascriptInterface
    public void setBrightness(final int level) {
        final int clamped = Math.max(5, Math.min(255, level));
        lastBrightness = clamped;
        activity.runOnUiThread(() -> {
            WindowManager.LayoutParams lp = activity.getWindow().getAttributes();
            lp.screenBrightness = clamped / 255.0f;
            activity.getWindow().setAttributes(lp);
        });
        try {
            Settings.System.putInt(activity.getContentResolver(), Settings.System.SCREEN_BRIGHTNESS, clamped);
        } catch (Exception ignored) {
            // Fallback via root if WRITE_SETTINGS not granted
            runSu("settings put system screen_brightness " + clamped);
        }
    }

    @JavascriptInterface
    public void setScreenSleep(final boolean asleep) {
        activity.runOnUiThread(() -> {
            WindowManager.LayoutParams lp = activity.getWindow().getAttributes();
            if (asleep) {
                lp.screenBrightness = 0.01f;
            } else {
                lp.screenBrightness = Math.max(0.05f, lastBrightness / 255.0f);
            }
            activity.getWindow().setAttributes(lp);
        });
    }

    @JavascriptInterface
    public String getDeviceInfo() {
        try {
            JSONObject obj = new JSONObject();
            obj.put("model", Build.MODEL);
            obj.put("androidVersion", Build.VERSION.RELEASE);
            obj.put("sdkInt", Build.VERSION.SDK_INT);

            String lanIp = "";
            String tailscaleIp = "";
            for (NetworkInterface nif : Collections.list(NetworkInterface.getNetworkInterfaces())) {
                if (!nif.isUp() || nif.isLoopback()) continue;
                for (InetAddress addr : Collections.list(nif.getInetAddresses())) {
                    if (!(addr instanceof Inet4Address)) continue;
                    String ip = addr.getHostAddress();
                    if (ip == null) continue;
                    if (ip.startsWith("100.")) {
                        if (tailscaleIp.isEmpty()) tailscaleIp = ip;
                    } else if (lanIp.isEmpty()) {
                        lanIp = ip;
                    }
                }
            }
            obj.put("lanIp", lanIp);
            obj.put("tailscaleIp", tailscaleIp);

            StatFs sf = new StatFs(activity.getFilesDir().getAbsolutePath());
            obj.put("freeBytes", sf.getAvailableBytes());
            obj.put("totalBytes", sf.getTotalBytes());
            return obj.toString();
        } catch (Exception e) {
            Log.e(TAG, "getDeviceInfo error", e);
            return "{}";
        }
    }

    @JavascriptInterface
    public void openWifiSettings() {
        activity.runOnUiThread(() -> {
            try {
                Intent i = new Intent(Settings.ACTION_WIFI_SETTINGS);
                i.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK);
                activity.startActivity(i);
            } catch (Exception e) {
                Log.e(TAG, "openWifiSettings error", e);
            }
        });
    }

    @JavascriptInterface
    public boolean launchApp(final String packageName) {
        try {
            Intent i = activity.getPackageManager().getLaunchIntentForPackage(packageName);
            if (i == null) return false;
            i.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK);
            activity.startActivity(i);
            return true;
        } catch (Exception e) {
            Log.e(TAG, "launchApp error: " + packageName, e);
            return false;
        }
    }

    @JavascriptInterface
    public void rebootDevice() {
        runSu("reboot");
    }

    private void runSu(final String cmd) {
        new Thread(() -> {
            try {
                Process p = Runtime.getRuntime().exec(new String[]{"su", "-c", cmd});
                p.waitFor();
            } catch (Exception e) {
                Log.w(TAG, "su cmd failed: " + cmd, e);
            }
        }).start();
    }
}
