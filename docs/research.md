# Research & references

Primary source: [`wesbos/photo-frame-dashboard`](https://github.com/wesbos/photo-frame-dashboard)
— the authoritative community repo for these cheap Frameo frames.

## CONFIRMED device identity (measured, not guessed)

**Rayswitch RSFM101**, Frameo app stock v1.29.14 (beta now v1.32.18).

From `getprop` / `su 0` on the live device:

| Item | Value |
| --- | --- |
| SoC | **Rockchip RK3126** (`ro.board.platform=rk312x`, `ro.hardware=rk30board`) |
| Android | **6.0.1** (Marshmallow, SDK 23) |
| Fingerprint | `rockchip/rk312x/rk312x:6.0.1/MXC89K/user.zcx.20260312.081202:user/release-keys` |
| Build | `MXC89K`, built 2026-03-12, builder `zcx` |
| Brand / model | `Rayswitch` / `Rayswitch_RSFM101` |
| RAM | ~1 GB (1,021,412 kB) |
| Storage | eMMC `mmcblk0`, **31.8 GB** total (32 GB) |
| WebView | 106.0.5249.126 |
| SELinux | **Permissive** |
| Root | **YES** — shipped SuperSU (`/system/xbin/su` + `/system/xbin/daemonsu`); `su 0 id` → `uid=0(root)` |
| Boot | `ro.bootmode=emmc`; no AVB / verified boot (pre-AVB Android 6) |
| USB | vendor `2207` (Rockchip), ADB product `0006` |

> **Correction to earlier notes:** we initially guessed RK3326 / Android 11 /
> no-root (the "S101" board) because "S101" appears in "RSFM101". **Wrong.**
> This is an **RK3126 / Android 6.0.1** unit. The RK3126 Frameo family is
> actually *better* for us — they ship with working root (`su`), accept ADB
> with no prompt, and have no verified boot to fight.

## Partition layout (eMMC, `mmcblk0`, 32 GB)

| Part | Name | Size | Blocks |
| --- | --- | --- | --- |
| p1 | uboot | 4 MB | 4096 |
| p2 | misc | 4 MB | 4096 |
| p3 | resource | 16 MB | 16384 |
| p4 | kernel | 12 MB | 12288 |
| p5 | boot | 22 MB | 22528 |
| p6 | recovery | 32 MB | 32768 |
| p7 | backup | 64 MB | 65536 |
| p8 | cache | 128 MB | 131072 |
| p9 | metadata | 16 MB | 16384 |
| p10 | kpanic | 4 MB | 4096 |
| p11 | system | 1.125 GB | 1179648 |
| p12 | radical_update | 64 MB | 65536 |
| p13 | userdata | ~28 GB | 29517824 |

`radical_update` (p12) is a vendor OTA partition — likely tied to the Adups
FOTA scheme. Flag for inspection.

## Vendor apps to remove

`pm list packages -s` → 59 system packages. Non-AOSP standouts:

| Package | Path | Notes |
| --- | --- | --- |
| `net.frameo.frame` | `/system/priv-app/Frameo` | Frameo, the default Home app |
| `com.adups.fota` | `/system/app/FotaUpdate/FotaUpdate.apk` | **Adups FOTA** — phones home (`adups.cn/.com`); prime removal target |
| `android.rockchip.update.service` | `/system/app/RKUpdateService` | Rockchip OTA updater |
| `com.cghs.stresstest` | `/system/priv-app/StressTest` | Factory test, runs as `system` |
| `com.DeviceTest` | `/system/priv-app/DeviceTest` | Factory test |
| `com.android.dreams.phototable` | (AOSP) | Daydream screensaver — not malicious, leave or repurpose |

No hidden root agent (`iotv`/`suyi`) found — only the legit `daemonsu`.

## Root

- **We have full root**: `su 0 id` → `uid=0(root) gid=0(root)`, context
  `u:r:toolbox:s0`. SuperSU `daemonsu` present, so it persists.
- SELinux **Permissive** — nothing enforced against us.
- No verified boot (Android 6, pre-AVB) — `/system` can be remounted `rw` and
  modified, and the boot image can be repacked without signature issues.

This is strictly better than the RK3326/Android 11 S101 board, which had **no**
root and an unlocked-but-untested bootloader.

## Network ADB

- `adb tcpip 5555` works; adbd listens on `0.0.0.0:5555`.
- `persist.adb.tcp.port=5555` set (survives reboots).
- **But**: the frame's Wi-Fi is on `192.168.9.220`, while our Mac is on
  `10.0.0.140` (different subnet; the Mac gateway `10.0.0.1` refuses to route).
  So `adb connect` is unreachable from the Mac for now. Options: move the frame
  onto the `10.0.0.x` SSID, or reach it from a host on `192.168.9.x`.

## Cable rules (confirmed on our unit + across the RK3126 index)

- USB-C to USB-C: **power only** (no CC pull-downs on the frame's receptacle).
- USB-A to USB-C (or USB-C→USB-A adapter/dongle): **works**. We got ADB up with
  a new dongle.

## Why issue #38 is NOT us

[Issue #38](https://github.com/wesbos/photo-frame-dashboard/issues/38) is a
**dead-end Allwinner A33** "DPF ZC002" with the same Frameo *app* version
(1.29.14) but no SystemUI → no authorize dialog → needed a FEL/U-Boot +
userdata-key write to root.

We are **Rockchip RK3126** (vendor `2207`, not Allwinner `1f3a`), and we
**already have root + a working prompt**. Not the same problem at all.

## Next steps (plan)

1. ✅ Bring-up / ADB — done (USB, root shell).
2. ✅ Inventory — done (this doc).
3. ✅ **Backup** — full boot chain + `/system` images, APKs, `build.prop`.
   See [`backups/MANIFEST.md`](../backups/MANIFEST.md) + `backups/SHA256SUMS`.
4. ✅ **Debloat** — 9 vendor apps physically removed from `/system` (below).
5. ⬜ **Install** our own photo viewer as the Home app.
6. ⬜ **Harden** — disable Frameo (kills remaining egress), block leftover
   domains at the router/DNS level, verify no outbound traffic.

## Debloat log (done 2026-11-22)

Removed 9 vendor apps. First `pm uninstall --user 0`, but they were
`PERSISTENT` (auto-restarted, new PIDs after every kill) and the APKs remained
on disk — so we remounted `/system` rw and physically `rm -rf`'d the APK dirs
(see [`scripts/debloat.sh`](../scripts/debloat.sh)). Verified clean after reboot.

| Package | Removed from |
| --- | --- |
| `com.adups.fota` (Adups FOTA) | `/system/app/FotaUpdate/` |
| `android.rockchip.update.service` (Rockchip OTA) | `/system/app/RKUpdateService/` |
| `com.cghs.stresstest` (factory stress test) | `/system/priv-app/StressTest/` |
| `com.DeviceTest` (factory test) | `/system/app/DeviceTest/` |
| `com.android.rk` (RkExplorer) | `/system/app/RkExplorer/` |
| `android.rk.RockVideoPlayer` | `/system/app/RkVideoPlayer/` |
| `com.android.rk.mediafloat` | `/system/app/MediaFloat/` |
| `com.android.apkinstaller` (RkApkinstaller) | `/system/app/RkApkinstaller/` |
| `com.android.inputdevices` | `/system/priv-app/InputDevices/` |

### Adups FOTA — what it was

- Backend: `my-ota-project.appspot.com` (Google App Engine) + a Google OAuth
  client ID.
- Permissions tell the story: `RECOVERY`, `REBOOT`, `WRITE_SECURE_SETTINGS`,
  `WRITE_SETTINGS`, `READ_PHONE_STATE`/`READ_PRIVILEGED_PHONE_STATE`,
  `SYSTEM_ALERT_WINDOW`, `INTERNAL_SYSTEM_WINDOW`, `INSTALL_LOCATION_PROVIDER`,
  `CHANGE_OVERLAY_PACKAGES`, and GCM push (`com.google.android.c2dm.permission.RECEIVE`).
- In short: a persistent, system-privileged remote-OTA agent with a push channel
  for remote commands. Now gone. APK retained at `backups/apks/com.adups.fota.apk`.

### Remaining network egress (after debloat, before disabling Frameo)

`netstat` after reboot shows the only outbound connections are from the
still-running **Frameo** app (uid 10015):

- Cloudflare-fronted (`104.26.15.72`, `172.67.73.26`) — Frameo's API/CDN.
- Google (`142.251.47.238`) — Google services (GCM/analytics/time).
- DigitalOcean VPS (`134.209.44.191`) — likely Frameo's own backend.

Disabling Frameo (step 6) should eliminate all of it.

## Key links

- [`docs/devices.md`](https://raw.githubusercontent.com/wesbos/photo-frame-dashboard/main/docs/devices.md) — device index (many RK3126/6.0.1 rows)
- [Issue #38](https://github.com/wesbos/photo-frame-dashboard/issues/38) — the *Allwinner* `1.29.14` unit (not ours)
- [Frameo: How to Enable ADB](https://support.frameo.com/hc/en-us/articles/6126183308434--How-to-Enable-ADB-on-Your-Frame)
