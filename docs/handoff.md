# Handoff — state as of 2026-11-22

## Where we are

The frame now runs **frmaeo** (our own app + server) as its Home app, with
Frameo disabled and the cloud-phoning vendor apps removed. It boots straight
into the frmaeo slideshow and is reachable over the local network.

## Device

- **Rayswitch RSFM101** — Rockchip RK3126, Android 6.0.1 (SDK 23), 1 GB RAM,
  32 GB eMMC.
- Fingerprint: `rockchip/rk312x/rk312x:6.0.1/MXC89K/user.zcx.20260312.081202`.
- ABI `armeabi-v7a` — matches the bundled Go server binary.
- Root via shipped SuperSU `su` (SELinux permissive). `adb shell` is `uid 2000`;
  use `su 0` for root.

## What's done

1. **Backup** — `backups/` holds verified images of the boot chain + `/system`
   (1.2 GB), all pulled APKs, and `build.prop`, hashed in
   `backups/SHA256SUMS`, documented in `backups/MANIFEST.md`.
2. **Debloat** — 9 vendor apps physically removed from `/system`:
   `com.adups.fota` (Adups FOTA), `android.rockchip.update.service` (Rockchip
   OTA), `com.cghs.stresstest`, `com.DeviceTest`, and the Rk* junk
   (`RkExplorer`, `RkVideoPlayer`, `MediaFloat`, `RkApkinstaller`,
   `InputDevices`). See `docs/research.md` and `scripts/debloat.sh`.
3. **App installed** — `com.frmaeo.kiosk` (`dist/frmaeo-kiosk.apk`). The Go
   server is bundled as `libfrmaeo.so` and runs as a subprocess; the WebView
   kiosk loads `http://127.0.0.1:8080/`.
4. **Home switch** — `net.frameo.frame` is disabled; frmaeo is now the sole
   Home app and boots automatically. Verified across two reboots.
5. **mDNS (`frame.local`)** — built-in pure-Go mDNS responder (`server/internal/mdns`)
   with `WifiManager.MulticastLock` in the kiosk app advertises `frame.local`
   (`A` record + `_http._tcp.local` PTR/SRV/TXT) using the live `wlan0` IPv4.
   Survives reboots without touching Android's `net.hostname`. Verified from
   Mac via `curl http://frame.local:8080/api/health`.
6. **Public Gallery, Categories & Image Search** — fixed Android 6.0.1 DNS
   (custom resolver using Wi-Fi DHCP DNS + `1.1.1.1`/`8.8.8.8`) and TLS
   (bundled up-to-date CA roots in `server/internal/publicfeed/cacerts.pem`).
   Added curated categories (`All Curated`, `Fine Art`, `Nature & Wildlife`,
   `Space & Cosmos`, `Architecture`, `Vintage & History`), public image search
   across Wikimedia Commons, The Met Museum, and NASA, and live SSE download
   progress bars.
7. **Custom Boot Splash & Animation** — replaced Frameo × Rayswitch branding:
   - `resource` partition (`logo.bmp` & `logo_kernel.bmp` patched in-place via
     `scripts/build_bootanim.py`, `rk-kernel.dtb` untouched, MD5 verified):
     displays *"Built by James Burgess with ♥"*.
   - `/system/media/bootanimation.zip` (24 fps, `1280×800`, stock backed up to
     `backups/bootanimation.stock.zip`): smoothly writes *"Thanks for being the
     best dad ever!"*, then transitions into an animated **Burgess (England)**
     family crest with golden halo, metallic sheen, and floating gold motes.
8. **Network ADB** — `persist.adb.tcp.port=5555` set; reachable at
   `10.0.0.131:5555`.

## Key values

| Item | Value |
| --- | --- |
| Family token (upload page) | `maple-1356` (auto-generated) |
| Server port | `8080` |
| Data dir (on frame) | `/data/user/0/com.frmaeo.kiosk/files/frmaeo` |
| LAN IP (current, DHCP) | `10.0.0.131` |
| mDNS hostname (current) | `frame.local` ✅ (verified over LAN) |
| APK | `dist/frmaeo-kiosk.apk` (signed with `android/debug.keystore`, pass `android`) |

## Open / not done

1. **Tailscale** — not installed yet. Planned as the remote-management path
   (see `docs/plan.md`). Caveat: Tailscale Android needs 6.0+, and login is a
   GUI flow on the touchscreen.
2. **Phone upload test** — `http://frame.local:8080/api/health` is verified
   from the Mac over LAN; do a quick upload test from a phone scanning the QR
   code on Settings → Add Photos.
3. **Family token** — change `maple-1356` to something the family will
   remember (set `FRMAEO_TOKEN` / pass `--token` to the server, or edit
   `settings.json`).

## Useful commands

```sh
# via USB
adb -s JMIBF91BUZ shell                  # shell (uid 2000)
adb -s JMIBF91BUZ shell su 0 <cmd>       # root

# via network
adb connect 10.0.0.131:5555

# server health (through adb forward)
adb -s JMIBF91BUZ forward tcp:8080 tcp:8080
curl http://127.0.0.1:8080/api/health

# rebuild + reinstall the APK
./scripts/deploy.sh
```

## Repo layout

- `README.md` — goals, status, approach.
- `docs/plan.md` — architecture, auth, networking, implementation plan.
- `docs/research.md` — device identity, vendor-app research, debloat log.
- `backups/` — partition images + APKs + hashes + manifest.
- `server/` — Go server (embedded web UI: kiosk + manage).
- `android/` — Android kiosk sources + debug keystore.
- `assets/` — enhanced Burgess (England) family crest (`burgess_crest.jpg`).
- `scripts/` — `build_apk.sh`, `deploy.sh`, `debloat.sh`, `build_bootanim.py`.
- `dist/` — built APK, custom `resource.custom.img`, and `bootanimation.zip` (gitignored).
