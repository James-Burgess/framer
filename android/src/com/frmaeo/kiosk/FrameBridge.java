package com.frmaeo.kiosk;

import android.app.Activity;
import android.content.Context;
import android.content.Intent;
import android.net.wifi.WifiInfo;
import android.net.wifi.WifiManager;
import android.os.Build;
import android.os.StatFs;
import android.provider.Settings;
import android.util.Log;
import android.view.WindowManager;
import android.webkit.JavascriptInterface;

import org.json.JSONObject;

import java.io.BufferedReader;
import java.io.File;
import java.io.InputStreamReader;
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

    /**
     * Collects a one-shot health snapshot for the on-frame debug screen.
     * Runs a single root shell (adb, ping) so it stays fast.
     */
    @JavascriptInterface
    public String getDiagnostics() {
        JSONObject out = new JSONObject();
        try {
            // Wi-Fi
            JSONObject wifi = new JSONObject();
            try {
                WifiManager wm = (WifiManager) activity.getApplicationContext().getSystemService(Context.WIFI_SERVICE);
                if (wm != null) {
                    wifi.put("enabled", wm.isWifiEnabled());
                    WifiInfo wi = wm.getConnectionInfo();
                    if (wi != null) {
                        String ssid = wi.getSSID();
                        if (ssid != null) ssid = ssid.replace("\"", "");
                        wifi.put("ssid", ssid == null || "<unknown ssid>".equals(ssid) ? "" : ssid);
                        wifi.put("rssi", wi.getRssi());
                        wifi.put("signal", WifiManager.calculateSignalLevel(wi.getRssi(), 5));
                        wifi.put("linkMbps", wi.getLinkSpeed());
                    }
                }
            } catch (Exception e) {
                wifi.put("error", e.getMessage());
            }
            out.put("wifi", wifi);

            // Memory + uptime (no root needed)
            String mem = readFile("/proc/meminfo");
            out.put("memTotalKb", parseMemKb(mem, "MemTotal:"));
            long avail = parseMemKb(mem, "MemAvailable:");
            if (avail <= 0) avail = parseMemKb(mem, "MemFree:") + parseMemKb(mem, "Cached:");
            out.put("memAvailKb", avail);
            String up = readFile("/proc/uptime").trim();
            if (!up.isEmpty()) out.put("deviceUptimeSec", (long) Double.parseDouble(up.split("\\s+")[0]));

            // Root snapshot
            String script =
                    "echo @@adbport; getprop service.adb.tcp.port; getprop persist.adb.tcp.port;" +
                    "echo @@adblisten; netstat -ltn 2>/dev/null | grep -c ':5555 ';" +
                    "echo @@ping; ping -c 1 -W 2 1.1.1.1 >/dev/null 2>&1 && echo ok || echo fail;" +
                    "echo @@end";
            String raw = execSu(script, 8000);
            out.put("rootOk", raw.contains("@@end"));

            String adbPort = section(raw, "adbport").trim();
            String adbListen = section(raw, "adblisten").trim();
            String ping = section(raw, "ping").trim();

            JSONObject adb = new JSONObject();
            String[] ports = adbPort.split("\\s+");
            adb.put("tcpPort", ports.length > 0 ? ports[0] : "");
            adb.put("persistPort", ports.length > 1 ? ports[1] : "");
            adb.put("listening", !adbListen.isEmpty() && !"0".equals(adbListen));
            out.put("adb", adb);

            out.put("internet", "ok".equals(ping));
        } catch (Exception e) {
            Log.e(TAG, "getDiagnostics error", e);
            try { out.put("error", e.getMessage()); } catch (Exception ignored) {}
        }
        return out.toString();
    }

    private static String section(String raw, String name) {
        String marker = "@@" + name + "\n";
        int start = raw.indexOf(marker);
        if (start < 0) return "";
        start += marker.length();
        int end = raw.indexOf("\n@@", start - 1);
        if (end < 0) end = raw.length();
        return start <= end ? raw.substring(start, end) : "";
    }

    private static long parseMemKb(String mem, String key) {
        for (String line : mem.split("\n")) {
            if (line.startsWith(key)) {
                try {
                    return Long.parseLong(line.substring(key.length()).trim().split("\\s+")[0]);
                } catch (Exception ignored) {
                }
            }
        }
        return 0;
    }

    private static String readFile(String path) {
        StringBuilder sb = new StringBuilder();
        try (BufferedReader br = new BufferedReader(new java.io.FileReader(new File(path)))) {
            String line;
            while ((line = br.readLine()) != null) sb.append(line).append('\n');
        } catch (Exception ignored) {
        }
        return sb.toString();
    }

    /** Runs a script as root, returning combined output; kills it after timeoutMs. */
    private static String execSu(String script, long timeoutMs) {
        final StringBuilder sb = new StringBuilder();
        try {
            final Process p = new ProcessBuilder("su", "0", "sh", "-c", script)
                    .redirectErrorStream(true)
                    .start();
            Thread reader = new Thread(() -> {
                try (BufferedReader br = new BufferedReader(new InputStreamReader(p.getInputStream()))) {
                    String line;
                    while ((line = br.readLine()) != null) {
                        synchronized (sb) {
                            sb.append(line.replace("\r", "")).append('\n');
                        }
                    }
                } catch (Exception ignored) {
                }
            });
            reader.start();
            reader.join(timeoutMs);
            if (reader.isAlive()) p.destroy();
        } catch (Exception e) {
            Log.w(TAG, "execSu failed", e);
        }
        synchronized (sb) {
            return sb.toString();
        }
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
