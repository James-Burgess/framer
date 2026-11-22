# frmaeo

Replace the stock Frameo software on a digital photo frame with our own
application that keeps all data local — no cloud, no telemetry, no accounts.

## Goal

We bought a Frameo digital photo frame (the "X Rayswitch" device). The
off-the-shelf firmware uploads photos through Frameo's cloud and app, which
means our pictures and metadata leave the device. We want to remove the stock
software and run our own app on the frame instead.

### What we want

- **Local-only photo display.** Photos are loaded directly onto the device
  (USB, SD, or local network) and never leave it.
- **No cloud dependency.** No Frameo account, no phone-app pairing, no
  telemetry or "phone home" calls.
- **Full control.** Our own slideshow / photo viewer, scheduling, and any
  features we decide to build.
- **Reproducible.** A documented process so another frame can be set up the
  same way.

### What we don't want

- Anything that requires a cloud server or third-party account.
- Silent uploads of photos, device IDs, Wi-Fi credentials, or usage data.

## Current status

**`frmaeo` is live on the device** as the sole Home app, with Frameo disabled,
vendor telemetry/OTA apps removed, `frame.local` mDNS working over LAN, hybrid
Public galleries + image search working, and a custom **Burgess** family boot
sequence replacing all Frameo × Rayswitch branding.

- Device: **Rayswitch RSFM101** (Rockchip **RK3126**, `armeabi-v7a`, Android **6.0.1**).
- **Root:** `su 0 id` → `uid=0(root)` (shipped SuperSU, `daemonsu` present), SELinux Permissive.
- **Network ADB:** `persist.adb.tcp.port=5555` set; reachable on LAN (`10.0.0.131:5555`) and USB (`JMIBF91BUZ`).
- **Local URL:** `http://frame.local:8080/` (Kiosk) and `http://frame.local:8080/manage` (Family Upload Portal).

Confirmed identity (measured, not guessed) — see
[`docs/research.md`](docs/research.md) for the full table:

| Item | Value |
| --- | --- |
| SoC | Rockchip **RK3126** (`rk312x` / `rk30board`, `armeabi-v7a`) |
| Android | **6.0.1** (SDK 23), fingerprint `…/rk312x:6.0.1/MXC89K/user.zcx.20260312…` |
| Display | 1280×800 IPS touchscreen (`160 dpi`, `ro.sf.hwrotation=90`) |
| RAM / storage | ~1 GB / 32 GB eMMC (`mmcblk0`, ~28 GB userdata) |
| WebView | 106.0.5249.126 |
| Root | Yes — SuperSU `su` + `daemonsu` |
| SELinux | Permissive |
| Home App | `com.frmaeo.kiosk` (`net.frameo.frame` disabled) |

> **Earlier note corrected:** we first guessed RK3326 / Android 11 / no-root
> ("S101" in "RSFM101" was a red herring). Reality is RK3126 / Android 6.0.1
> **with root** — a strictly easier starting point.

### How we got here (cable + ADB switch)

- **USB-C to USB-C: charge only.** The frame's USB-C receptacle lacks the CC
  pull-downs, so the Mac never sees data. Confirmed on our unit.
- **USB-A to USB-C (or USB-C→USB-A dongle): works.** The working dongle was the
  fix. A stacked hub chain was flaky (constant re-enumeration → `offline`).
- The ADB switch is **not** "tap build number 7×". It's Frameo's official
  route: **Settings → About → Enable Beta Program → ADB Access on**. Note:
  joining beta triggered a Frameo update (v1.29.14 → v1.32.18 beta).

### Open questions

- [ ] Get the frame onto the Mac's subnet (`10.0.0.x`) so network ADB is
      reachable — or accept USB-only for now.
- [ ] What does the vendor updater (`com.adups.fota`) actually send? (grab the
      APK and inspect its URLs/domains)
- [ ] What is the `radical_update` partition (p12) for?
- [ ] Do we need `/system` writes, or is `pm` debloat + our own Home app enough?

## Approach

1. ✅ **Bring-up** — USB-A→USB-C dongle, Beta Program → ADB Access, prompt accepted.
2. ✅ **Inventory** — `getprop` / `pm list packages` / partition map (see
   [`docs/research.md`](docs/research.md)).
3. ✅ **Backup** — boot chain + `/system` images, APKs, `build.prop`; hashes in
   [`backups/SHA256SUMS`](backups/SHA256SUMS), manifest in
   [`backups/MANIFEST.md`](backups/MANIFEST.md).
4. ✅ **Debloat** — 9 vendor apps removed from `/system` (Adups FOTA, Rockchip
   OTA, factory tests, Rk* junk).
5. ✅ **Install** — `frmaeo` (Go server + Kiosk WebUI + Family Upload Portal + Android
   WebView Kiosk APK + built-in `frame.local` mDNS) set as Home app; `net.frameo.frame` disabled.
6. ✅ **Custom Boot Experience** — U-Boot/kernel `resource` splash (*"Built by James Burgess with ♥"*)
   and 24 fps animated boot screen (*"Thanks for being the best dad ever!"* → animated **Burgess** crest).
7. ⬜ **Remote (optional)** — install Tailscale APK for remote family uploads from outside Dad's LAN.

## Quick start

```sh
# Local dev preview on Mac (Kiosk at :8080, Family Upload at :8080/manage)
cd server && GOTOOLCHAIN=local go run . --addr :8080 --data ./data

# Build signed single-file Android APK (bundles static ARMv7 Go binary)
./scripts/build_apk.sh

# Build & deploy straight to the frame via ADB
./scripts/deploy.sh

# Regenerate custom U-Boot resource.img splash + animated bootanimation.zip
python3 scripts/build_bootanim.py
```

## Repo layout

- `README.md` — project overview, status, and quick-start commands.
- `docs/` — `plan.md`, `research.md`, and `handoff.md`.
- `server/` — single-binary Go backend (`CGO_ENABLED=0`) with embedded Kiosk & Manage WebUIs, EXIF + offline geocoder, built-in `frame.local` mDNS responder, and pull-only Public gallery + search engine.
- `android/` — minimal Android 6.0.1 WebView Home launcher (`FrameBridge`, `ServerManager`, `MulticastLock`).
- `assets/` — enhanced Burgess (England) family crest master image.
- `scripts/` — `debloat.sh`, `build_apk.sh`, `deploy.sh`, `build_bootanim.py`.
- `backups/` — backup manifest, SHA-256 checksums, and stock `build.prop` (raw `.img` files gitignored).
