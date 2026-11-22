package com.frmaeo.kiosk;

import android.content.Context;
import android.net.DhcpInfo;
import android.net.wifi.WifiManager;
import android.util.Log;

import java.io.BufferedReader;
import java.io.File;
import java.io.InputStreamReader;
import java.net.HttpURLConnection;
import java.net.URL;

public class ServerManager {
    private static final String TAG = "frmaeo-server";
    public static final int PORT = 8080;

    private final Context context;
    private volatile boolean running = false;
    private Process process;
    private Thread supervisorThread;

    public ServerManager(Context context) {
        this.context = context.getApplicationContext();
    }

    public synchronized void start() {
        if (running) return;
        running = true;

        final String binPath = new File(context.getApplicationInfo().nativeLibraryDir, "libfrmaeo.so").getAbsolutePath();
        final File dataDir = new File(context.getFilesDir(), "frmaeo");
        if (!dataDir.exists()) {
            //noinspection ResultOfMethodCallIgnored
            dataDir.mkdirs();
        }

        supervisorThread = new Thread(() -> {
            while (running) {
                try {
                    Log.i(TAG, "Starting Go server: " + binPath + " data=" + dataDir.getAbsolutePath());
                    ProcessBuilder pb = new ProcessBuilder(
                            binPath,
                            "--addr", ":" + PORT,
                            "--data", dataDir.getAbsolutePath()
                    );
                    String dns = detectWifiDns();
                    if (!dns.isEmpty()) {
                        pb.environment().put("FRMAEO_DNS", dns);
                    }
                    pb.redirectErrorStream(true);
                    process = pb.start();

                    try (BufferedReader br = new BufferedReader(new InputStreamReader(process.getInputStream()))) {
                        String line;
                        while ((line = br.readLine()) != null) {
                            Log.i(TAG, line);
                        }
                    }
                    int code = process.waitFor();
                    Log.w(TAG, "Server exited with code " + code);
                } catch (Exception e) {
                    Log.e(TAG, "Server error", e);
                }
                if (running) {
                    try {
                        Thread.sleep(2000);
                    } catch (InterruptedException ignored) {
                        break;
                    }
                }
            }
        }, "frmaeo-supervisor");
        supervisorThread.start();
        startTailscaleIfInstalled();
    }

    private void startTailscaleIfInstalled() {
        final File script = new File("/data/tailscale/start.sh");
        if (!script.exists()) return;
        new Thread(() -> {
            try {
                Log.i(TAG, "Starting native tailscaled via " + script.getAbsolutePath());
                Process p = new ProcessBuilder("su", "0", "sh", script.getAbsolutePath())
                        .redirectErrorStream(true)
                        .start();
                try (BufferedReader br = new BufferedReader(new InputStreamReader(p.getInputStream()))) {
                    String line;
                    while ((line = br.readLine()) != null) {
                        Log.i(TAG, "[tailscale] " + line);
                    }
                }
                int code = p.waitFor();
                Log.i(TAG, "tailscale start.sh exited with code " + code);
            } catch (Exception e) {
                Log.w(TAG, "Failed to start tailscaled", e);
            }
        }, "frmaeo-tailscale").start();
    }

    public synchronized void stop() {
        running = false;
        if (process != null) {
            process.destroy();
            process = null;
        }
    }

    public boolean waitForReady(int timeoutMs) {
        long deadline = System.currentTimeMillis() + timeoutMs;
        while (System.currentTimeMillis() < deadline) {
            try {
                HttpURLConnection conn = (HttpURLConnection) new URL("http://127.0.0.1:" + PORT + "/api/health").openConnection();
                conn.setConnectTimeout(500);
                conn.setReadTimeout(500);
                int code = conn.getResponseCode();
                conn.disconnect();
                if (code == 200) return true;
            } catch (Exception ignored) {
            }
            try {
                Thread.sleep(200);
            } catch (InterruptedException e) {
                return false;
            }
        }
        return false;
    }

    private String detectWifiDns() {
        try {
            WifiManager wm = (WifiManager) context.getSystemService(Context.WIFI_SERVICE);
            if (wm == null) return "";
            DhcpInfo dhcp = wm.getDhcpInfo();
            if (dhcp == null) return "";
            StringBuilder sb = new StringBuilder();
            if (dhcp.dns1 != 0) sb.append(intToIp(dhcp.dns1));
            if (dhcp.dns2 != 0) {
                if (sb.length() > 0) sb.append(",");
                sb.append(intToIp(dhcp.dns2));
            }
            return sb.toString();
        } catch (Exception e) {
            return "";
        }
    }

    private static String intToIp(int i) {
        return (i & 0xFF) + "." + ((i >> 8) & 0xFF) + "." + ((i >> 16) & 0xFF) + "." + ((i >> 24) & 0xFF);
    }
}
